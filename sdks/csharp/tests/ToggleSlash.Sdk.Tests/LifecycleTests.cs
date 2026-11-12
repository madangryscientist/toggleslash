using System.Diagnostics;
using System.Net.Http;
using ToggleSlash.Sdk.Tests.Support;

namespace ToggleSlash.Sdk.Tests;

public class LifecycleTests
{
    private static readonly object Ctx = new Dictionary<string, object?> { ["key"] = "u" };

    [Fact]
    public async Task AClientThatIsNeverClosedDoesNotKeepTheProcessFromExiting()
    {
        var dll = Path.GetFullPath(Path.Combine(AppContext.BaseDirectory, "..", "..", "ToggleSlash.Sdk.NeverClosed", "debug", "ToggleSlash.Sdk.NeverClosed.dll"));
        Assert.True(File.Exists(dll), dll);
        var start = new ProcessStartInfo(Environment.ProcessPath!, $"\"{dll}\"") { RedirectStandardOutput = true, RedirectStandardError = true };
        using var process = Process.Start(start)!;
        var stdout = process.StandardOutput.ReadToEndAsync();
        var stderr = process.StandardError.ReadToEndAsync();
        using var limit = new CancellationTokenSource(TimeSpan.FromSeconds(60));
        await process.WaitForExitAsync(limit.Token);
        Assert.Equal(0, process.ExitCode);
        Assert.Contains("returning from Main", await stdout, StringComparison.Ordinal);
        Assert.Equal(string.Empty, await stderr);
    }

    [Fact]
    public async Task EvaluationAnswersWhileARulesetRequestHangsAndWhileAUsageFlushHangs()
    {
        var rulesetCalls = 0;
        var hangEvents = 0;
        using var server = new LoopbackServer(async (r, c, stop) =>
        {
            if (r.Path == "/sdk/v1/ruleset" && Interlocked.Increment(ref rulesetCalls) == 1)
            {
                await c.SendAsync(200, Rulesets.Ruleset(Rulesets.Flag("a", "true")), ("ETag", "\"v\""));
            }
            else
            {
                if (r.Path == "/sdk/v1/events")
                {
                    Interlocked.Increment(ref hangEvents);
                }

                await c.HangAsync(stop);
            }
        });
        using var client = Harness.Client(server, o => o.RequestTimeout = TimeSpan.FromSeconds(2));
        Assert.True(await client.ReadyAsync());
        await Wait.UntilAsync(() => rulesetCalls >= 2, "a hanging ruleset request");
        Assert.True(TimeIt(client) < TimeSpan.FromSeconds(5));
        await Wait.UntilAsync(() => Volatile.Read(ref hangEvents) >= 1, "a hanging usage flush");
        Assert.True(TimeIt(client) < TimeSpan.FromSeconds(5));
        Assert.True(client.Status().LastSyncAt is not null);

        // Close waits for the flush in flight and sends the final one: each ends at the request timeout, so about twice that.
        var watch = Stopwatch.StartNew();
        await Wait.WithinAsync(client.CloseAsync(), "close", 20_000);
        Assert.True(watch.Elapsed < TimeSpan.FromSeconds(8), $"close took {watch.Elapsed}");
    }

    private static TimeSpan TimeIt(ToggleSlashClient client)
    {
        var watch = Stopwatch.StartNew();
        for (var i = 0; i < 2000; i++)
        {
            Assert.True(client.BoolVariation("a", Ctx, false));
            _ = client.Status();
        }

        return watch.Elapsed;
    }

    [Fact]
    public async Task ATlsServerWithALocalCertificateIsReachableAndTheDefaultRefusesIt()
    {
        using var server = new LoopbackServer(Rulesets.Backend(() => Rulesets.Ruleset(Rulesets.Flag("a", "true")), () => "\"v\""), tls: true);
        using var trusting = new SocketsHttpHandler { SslOptions = { RemoteCertificateValidationCallback = (_, _, _, _) => true } };
        using var client = Harness.Client(server, o => o.Handler = trusting);
        Assert.True(await client.ReadyAsync());
        Assert.True(client.BoolVariation("a", Ctx, false));

        using var strict = Harness.Client(server);
        Assert.False(await strict.ReadyAsync());
        Assert.Equal("network", strict.Status().LastError);
    }

    [Fact]
    public async Task CloseIsIdempotentAndEvaluationStillAnswersAfterwards()
    {
        using var server = new LoopbackServer(Rulesets.Backend(() => Rulesets.Ruleset(Rulesets.Flag("a", "true")), () => "\"v\""));
        var client = Harness.Client(server);
        Assert.True(await client.ReadyAsync());
        client.BoolVariation("a", Ctx, false);
        var posts = server.To("/sdk/v1/events").Count;
        await Task.WhenAll(client.CloseAsync(), client.CloseAsync(), Task.Run(client.Close));
        client.Close();
        Assert.Equal(StreamState.closed, client.Status().Stream);
        Assert.True(client.BoolVariation("a", Ctx, false));
        Assert.True(server.To("/sdk/v1/events").Count >= posts);
        var after = server.Requests.Count;
        client.BoolVariation("a", Ctx, false);
        await Task.Delay(150);
        Assert.Equal(after, server.Requests.Count);
    }
}
