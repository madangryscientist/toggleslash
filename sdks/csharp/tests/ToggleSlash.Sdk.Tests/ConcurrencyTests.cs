using System.Collections.Concurrent;
using System.Diagnostics;
using System.Text.Json;

namespace ToggleSlash.Sdk.Tests;

/// <summary>
/// Contract 6 and 5.2. .NET has no race detector, so the many-thread tests here stand in for one: they run every
/// operation from many threads while the ruleset swaps under them and the client closes, and fail on any raise,
/// hang or torn read.
/// </summary>
public sealed class ConcurrencyTests
{
    private static readonly Context User = Context.ForKey("u1");

    private static async Task<HttpResponseMessage> Churn(Seen request, int[] counter)
    {
        switch (request.Path)
        {
            case Reply.Ruleset:
                var n = Interlocked.Increment(ref counter[0]);
                return await Reply.Rules(Flags.Constant("f", "true", n), $"\"v{n}\"");
            default:
                return await Reply.Status(202);
        }
    }

    [Fact]
    public async Task Every_operation_from_many_threads_with_close_racing_does_not_raise_or_hang()
    {
        var counter = new int[1];
        var errors = new Errors();
        var server = new FakeServer(TimeProvider.System, (r, _) => Churn(r, counter));
        var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = "http://sdk.invalid",
            SdkKey = "key",
            Stream = false,
            PollInterval = TimeSpan.FromMilliseconds(2),
            FlushInterval = TimeSpan.FromMilliseconds(3),
            OnError = errors.Add,
            Hooks = new Hooks { AfterEvaluation = e => _ = e.Etag },
            Handler = server,
        });
        Assert.True(await client.ReadyAsync());

        var raised = new ConcurrentQueue<Exception>();
        var stop = new CancellationTokenSource();
        using var start = new ManualResetEventSlim();
        var workers = Enumerable.Range(0, 24).Select(i => Task.Factory.StartNew(() =>
        {
            start.Wait();
            try
            {
                while (!stop.IsCancellationRequested)
                {
                    switch (i % 8)
                    {
                        case 0: client.BoolVariation("f", User, false); break;
                        case 1: client.BoolVariationDetail("f", User, false); break;
                        case 2: client.StringVariation("f", User, "x"); break;
                        case 3: client.IntVariationDetail("f", User, 0); break;
                        case 4: client.DoubleVariation("f", User, 0); break;
                        case 5: client.JsonVariation("f", User, default); break;
                        case 6: _ = client.Status; break;
                        default: client.ReadyAsync().Wait(); break;
                    }
                }
            }
            catch (Exception error)
            {
                raised.Enqueue(error);
            }
        }, TaskCreationOptions.LongRunning)).ToArray();

        start.Set();
        await Task.Delay(300);
        // Two closes at once, while the workers keep calling.
        var closers = new[] { Task.Run(client.Close), Task.Run(client.Close), Task.Run(() => client.CloseAsync().GetAwaiter().GetResult()) };
        await Task.WhenAll(closers).WaitAsync(TimeSpan.FromSeconds(15));
        await Task.Delay(100);
        stop.Cancel();
        await Task.WhenAll(workers).WaitAsync(TimeSpan.FromSeconds(15));

        Assert.Empty(raised);
        Assert.Equal(StreamState.Closed, client.Status.Stream);
        // Ready had settled true before close; a settled answer does not change.
        Assert.True(await client.ReadyAsync());
        Assert.Empty(errors.All);
        // After close the answer is still a fallback or a value, never a raise.
        _ = client.BoolVariation("f", User, true);
        Assert.True(counter[0] > 5, "the ruleset never swapped under the readers");
    }

    [Fact]
    public async Task A_hook_never_sees_an_etag_and_a_version_from_two_rulesets()
    {
        var counter = new int[1];
        var torn = new ConcurrentQueue<string>();
        var seenVersions = new ConcurrentDictionary<double, bool>();
        var server = new FakeServer(TimeProvider.System, (r, _) => Churn(r, counter));
        var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = "http://sdk.invalid",
            SdkKey = "key",
            Stream = false,
            PollInterval = TimeSpan.FromMilliseconds(1),
            Hooks = new Hooks
            {
                AfterEvaluation = e =>
                {
                    seenVersions[e.FlagVersion ?? -1] = true;
                    if (e.Etag != $"\"v{e.FlagVersion}\"") torn.Enqueue($"{e.Etag} with version {e.FlagVersion}");
                },
            },
            Handler = server,
        });
        Assert.True(await client.ReadyAsync());
        var stop = new CancellationTokenSource();
        var readers = Enumerable.Range(0, 8).Select(_ => Task.Factory.StartNew(() =>
        {
            while (!stop.IsCancellationRequested) client.BoolVariation("f", User, false);
        }, TaskCreationOptions.LongRunning)).ToArray();
        await Wait.Until(() => seenVersions.Count >= 50, "fifty different rulesets to be read");
        stop.Cancel();
        await Task.WhenAll(readers).WaitAsync(TimeSpan.FromSeconds(15));
        await client.CloseAsync();
        Assert.Empty(torn);
    }

    [Fact]
    public async Task Evaluation_returns_within_a_bound_while_the_first_ruleset_request_hangs()
    {
        var server = new FakeServer(TimeProvider.System, (_, token) => Reply.Hang(token));
        var client = ToggleSlashClient.Create(new ClientOptions { Url = "http://sdk.invalid", SdkKey = "key", Handler = server });
        var watch = Stopwatch.StartNew();
        var detail = client.BoolVariationDetail("f", User, true);
        Assert.True(watch.ElapsedMilliseconds < 500, $"took {watch.ElapsedMilliseconds} ms");
        Assert.Equal(ErrorCode.NotReady, detail.Reason.Code);
        Assert.True(detail.Value);
        var closing = Stopwatch.StartNew();
        client.Close();
        Assert.True(closing.ElapsedMilliseconds < 3000, $"close took {closing.ElapsedMilliseconds} ms with a request hanging");
    }

    [Fact]
    public async Task Evaluation_returns_within_a_bound_while_a_usage_flush_hangs()
    {
        var server = new FakeServer(TimeProvider.System, (r, token) => r.Path == Reply.Events ? Reply.Hang(token) : Reply.Rules(Flags.Constant("f", "true"), "\"v1\""));
        var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = "http://sdk.invalid",
            SdkKey = "key",
            Stream = false,
            FlushInterval = TimeSpan.FromMilliseconds(20),
            RequestTimeout = TimeSpan.FromSeconds(1),
            Handler = server,
        });
        Assert.True(await client.ReadyAsync());
        client.BoolVariation("f", User, false);
        await Wait.Until(() => server.To(Reply.Events).Count > 0, "the flush to start and hang");
        var watch = Stopwatch.StartNew();
        for (var i = 0; i < 1000; i++) Assert.True(client.BoolVariation("f", User, false));
        Assert.True(watch.ElapsedMilliseconds < 1000, $"1000 evaluations took {watch.ElapsedMilliseconds} ms during a hung flush");
        // Close waits for the running flush and sends the last one, each bounded by the request timeout (contract 11).
        var closing = Stopwatch.StartNew();
        await client.CloseAsync().WaitAsync(TimeSpan.FromSeconds(10));
        Assert.True(closing.Elapsed < TimeSpan.FromSeconds(5), $"close took {closing.Elapsed}");
    }

    [Fact]
    public async Task A_hook_and_an_onError_can_call_back_into_the_client()
    {
        var server = new FakeServer(TimeProvider.System, (r, _) => r.Path == Reply.Ruleset ? Reply.Rules(Flags.Constant("f", "true"), "\"v1\"") : Reply.Status(500));
        var hookCalls = 0;
        var errorCalls = 0;
        ToggleSlashClient? self = null;
        var inHook = new ThreadLocal<bool>();
        var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = "http://sdk.invalid",
            SdkKey = "key",
            Stream = false,
            FlushInterval = TimeSpan.FromMilliseconds(10),
            Hooks = new Hooks
            {
                AfterEvaluation = hookEvent =>
                {
                    _ = hookEvent.FlagKey;
                    // The hook's own variation call runs the hook again; the flag ends that on the second turn.
                    if (inHook.Value) return;
                    inHook.Value = true;
                    try
                    {
                        _ = self!.Status;
                        self.BoolVariation("f", User, false);
                        Interlocked.Increment(ref hookCalls);
                    }
                    finally
                    {
                        inHook.Value = false;
                    }
                },
            },
            OnError = error =>
            {
                _ = error.Message;
                _ = self!.Status;
                self.BoolVariation("f", User, false);
                Interlocked.Increment(ref errorCalls);
            },
            Handler = server,
        });
        self = client;
        Assert.True(await client.ReadyAsync());
        var work = Task.Run(() => client.BoolVariation("f", User, false));
        await work.WaitAsync(TimeSpan.FromSeconds(5));
        await Wait.Until(() => Volatile.Read(ref errorCalls) > 0, "the failed flush to reach onError");
        Assert.True(Volatile.Read(ref hookCalls) > 0);
        await client.CloseAsync().WaitAsync(TimeSpan.FromSeconds(5));
    }

    [Fact]
    public async Task Close_called_from_inside_onError_does_not_wait_for_itself()
    {
        var server = new FakeServer(TimeProvider.System, (_, _) => Reply.Status(500));
        ToggleSlashClient? self = null;
        var returned = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = "http://sdk.invalid",
            SdkKey = "key",
            OnError = error =>
            {
                _ = error.Message;
                self!.Close();
                returned.TrySetResult();
            },
            Handler = server,
        });
        self = client;
        await returned.Task.WaitAsync(TimeSpan.FromSeconds(5));
        await client.CloseAsync().WaitAsync(TimeSpan.FromSeconds(5));
        Assert.Equal(StreamState.Closed, client.Status.Stream);
    }

    [Fact]
    public async Task An_evaluation_that_raises_gives_the_fallback_evaluation_failed_and_one_onError()
    {
        var server = new FakeServer(TimeProvider.System, (r, _) => r.Path == Reply.Ruleset ? Reply.Rules(Flags.Constant("f", "true"), "\"v1\"") : Reply.Status(202));
        var errors = new Errors();
        var boom = new InvalidOperationException("the evaluator broke");
        var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = "http://sdk.invalid",
            SdkKey = "key",
            Stream = false,
            OnError = errors.Add,
            Evaluate = (_, _, _) => throw boom,
            Handler = server,
        });
        Assert.True(await client.ReadyAsync());
        var detail = client.BoolVariationDetail("f", User, true);
        Assert.True(detail.Value);
        Assert.Null(detail.VariationIndex);
        Assert.Equal(ReasonKind.Error, detail.Reason.Kind);
        Assert.Equal(ErrorCode.EvaluationFailed, detail.Reason.Code);
        Assert.Same(boom, Assert.Single(errors.All));
        await client.CloseAsync();
        // Nothing was served, so nothing was counted and nothing is sent.
        Assert.Empty(server.To(Reply.Events));
    }
}
