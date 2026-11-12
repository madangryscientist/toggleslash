using System.Diagnostics;
using System.Globalization;
using ToggleSlash.Sdk.Tests.Support;

namespace ToggleSlash.Sdk.Tests;

public class ConcurrencyTests
{
    private static readonly object Ctx = new Dictionary<string, object?> { ["key"] = "u" };

    [Fact]
    public async Task AHooksEtagAndFlagVersionBelongToTheSameRulesetUnderConcurrentSwaps()
    {
        var counter = 0;
        // Each answer is a new ruleset whose tag is "<n>" and whose flag version is n.
        using var server = new LoopbackServer(async (r, c, _) =>
        {
            if (r.Path != "/sdk/v1/ruleset")
            {
                await c.SendAsync(202, "{}");
                return;
            }

            var n = Interlocked.Increment(ref counter);
            await c.SendAsync(200, Rulesets.Ruleset(Rulesets.Flag("a", "true", n)), ("ETag", $"\"{n.ToString(CultureInfo.InvariantCulture)}\""));
        });
        var bad = 0;
        var seen = 0;
        var versions = new HashSet<long>();
        using var client = Harness.Client(server, o =>
        {
            o.PollInterval = TimeSpan.FromMilliseconds(1);
            o.AfterEvaluation = e =>
            {
                if (e.FlagVersion is not { } version)
                {
                    return;
                }

                Interlocked.Increment(ref seen);
                lock (versions)
                {
                    versions.Add(version);
                }

                if (e.Etag != $"\"{version.ToString(CultureInfo.InvariantCulture)}\"")
                {
                    Interlocked.Increment(ref bad);
                }
            };
        });
        Assert.True(await client.ReadyAsync());
        var watch = Stopwatch.StartNew();
        while (watch.Elapsed < TimeSpan.FromSeconds(1.5))
        {
            client.BoolVariation("a", Ctx, false);
        }

        Assert.Equal(0, bad);
        Assert.True(seen > 100);
        lock (versions)
        {
            Assert.True(versions.Count > 5, $"only {versions.Count} rulesets were swapped in");
        }
    }

    [Fact]
    public async Task StatusNeverShowsALastErrorBesideAConnectedStream()
    {
        using var server = new LoopbackServer(Rulesets.Backend(
            () => Rulesets.Ruleset(Rulesets.Flag("a", "true")),
            () => "\"v\"",
            stream: async (_, c, _) =>
            {
                await c.HeadAsync(200, ("Content-Type", "text/event-stream"));
                await c.WriteAsync("event: connected\ndata: \"v\"\n\n");
                await Task.Delay(1);
                c.Reset();
            }));
        using var client = Harness.Client(server, o =>
        {
            o.Stream = true;
            o.PollInterval = TimeSpan.FromMilliseconds(2);
        });
        var violations = 0;
        var connectedSeen = 0;
        var watch = Stopwatch.StartNew();
        while (watch.Elapsed < TimeSpan.FromSeconds(2))
        {
            var status = client.Status();
            if (status.Stream == StreamState.connected)
            {
                connectedSeen++;
                if (status.LastError is not null)
                {
                    violations++;
                }
            }
        }

        Assert.Equal(0, violations);
        Assert.True(connectedSeen > 0);
        Assert.True(server.To("/sdk/v1/stream").Count > 5);
    }

    [Fact]
    public async Task CallbacksThatCallBackIntoTheClientDoNotDeadlock()
    {
        using var server = new LoopbackServer(async (_, c, _) => await c.SendAsync(500, "no"));
        var inside = new TaskCompletionSource<(bool Ready, string Status)>(TaskCreationOptions.RunContinuationsAsynchronously);
        ToggleSlashClient? holder = null;
        using var client = Harness.Client(server, o => o.OnError = unused =>
        {
            var c = holder;
            if (c is null)
            {
                return;
            }

            _ = c.Status();
            _ = c.BoolVariation("a", Ctx, false);
            var ready = c.Ready();
            c.Close();
            inside.TrySetResult((ready, c.Status().Stream.ToString()));
        });
        holder = client;
        var (ready, stream) = await Wait.WithinAsync(inside.Task, "a callback that calls Ready and Close");
        Assert.False(ready);
        Assert.Equal("closed", stream);
        Assert.False(await Wait.WithinAsync(client.ReadyAsync(), "ready"));
        await Wait.WithinAsync(client.CloseAsync(), "close after a close from a callback");
    }

    [Fact]
    public async Task AHookThatCallsBackIntoTheClientDoesNotDeadlock()
    {
        using var server = new LoopbackServer(Rulesets.Backend(() => Rulesets.Ruleset(Rulesets.Flag("a", "true")), () => "\"v\""));
        var depth = 0;
        var seen = 0;
        ToggleSlashClient? holder = null;
        using var client = Harness.Client(server, o => o.AfterEvaluation = unused =>
        {
            Interlocked.Increment(ref seen);
            if (holder is { } c && Interlocked.Exchange(ref depth, 1) == 0)
            {
                _ = c.Status();
                _ = c.Ready();
                _ = c.BoolVariation("a", Ctx, false);
                Volatile.Write(ref depth, 0);
            }
        });
        holder = client;
        Assert.True(await client.ReadyAsync());
        Assert.True(await Task.Run(() => client.BoolVariation("a", Ctx, false)).WaitAsync(TimeSpan.FromSeconds(20)));
        Assert.True(seen >= 2);
    }

    [Fact]
    public async Task ATaskHookThatFailsLaterGoesToOnErrorAndChangesNeitherTheAnswerNorTheCount()
    {
        using var server = new LoopbackServer(Rulesets.Backend(() => Rulesets.Ruleset(Rulesets.Flag("a", "true")), () => "\"v\""));
        var errors = new List<Exception>();
        using var client = Harness.Client(server, o =>
        {
            o.FlushInterval = TimeSpan.FromHours(1);
            o.PollInterval = TimeSpan.FromHours(1);
            o.AfterEvaluationAsync = async _ =>
            {
                await Task.Delay(20);
                throw new InvalidOperationException("later");
            };
            o.OnError = e => { lock (errors) { errors.Add(e); } };
        });
        Assert.True(await client.ReadyAsync());
        var detail = client.BoolVariationDetail("a", Ctx, false);
        Assert.True(detail.Value);
        Assert.Equal(0, detail.VariationIndex);
        await Wait.UntilAsync(() => { lock (errors) { return errors.Count == 1; } }, "the hook's later failure");
        lock (errors)
        {
            Assert.Equal("later", errors[0].Message);
        }

        await client.FlushAsync(false);
        var posts = server.To("/sdk/v1/events");
        Assert.Single(posts);
        Assert.Contains("\"count\":1", posts[0].Body, StringComparison.Ordinal);
    }

    [Fact]
    public async Task AThrowingHookAndAThrowingOnErrorChangeNothing()
    {
        using var server = new LoopbackServer(Rulesets.Backend(() => Rulesets.Ruleset(Rulesets.Flag("a", "true")), () => "\"v\""));
        var reported = 0;
        using var client = Harness.Client(server, o =>
        {
            o.AfterEvaluation = _ => throw new InvalidOperationException("hook");
            o.OnError = _ =>
            {
                Interlocked.Increment(ref reported);
                throw new InvalidOperationException("on error");
            };
        });
        Assert.True(await client.ReadyAsync());
        Assert.True(client.BoolVariation("a", Ctx, false));
        Assert.Equal(1, reported);
    }
}
