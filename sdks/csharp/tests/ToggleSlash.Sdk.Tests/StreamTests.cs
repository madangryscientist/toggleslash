using System.Diagnostics;
using ToggleSlash.Sdk.Tests.Support;

namespace ToggleSlash.Sdk.Tests;

/// <summary>The stream's 60 s watchdog (7.2), run through the internal seam on a real loopback socket.</summary>
public class StreamTests
{
    private static readonly TimeSpan Watchdog = TimeSpan.FromMilliseconds(1500);

    private static LoopbackServer Serve(Func<Connection, CancellationToken, Task> firstStream) =>
        new(Rulesets.Backend(
            () => Rulesets.Ruleset(Rulesets.Flag("a", "true")),
            () => "\"v\"",
            stream: async (_, c, stop) =>
            {
                await c.HeadAsync(200, ("Content-Type", "text/event-stream"));
                await firstStream(c, stop);
            }));

    [Fact]
    public async Task AQuietStreamIsDroppedAsATimeoutAfterTheWatchdog()
    {
        var wrote = new Stopwatch();
        using var server = Serve(async (c, stop) =>
        {
            // Started before the write: the client cannot have read the event earlier, so the quiet time it saw is at most this.
            wrote.Restart();
            await c.WriteAsync("event: connected\ndata: \"v\"\n\n");
            await c.HangAsync(stop);
        });
        using var client = Harness.Client(server, o =>
        {
            o.Stream = true;
            o.StreamWatchdog = Watchdog;
            o.PollInterval = TimeSpan.FromHours(1);
        });
        Assert.True(await client.ReadyAsync());
        await Wait.UntilAsync(() => client.Status().LastError == "timeout", "the watchdog");
        Assert.True(wrote.Elapsed >= Watchdog * 0.9, $"dropped after {wrote.Elapsed}");
        Assert.NotEqual(StreamState.connected, client.Status().Stream);
    }

    [Fact]
    public async Task AKeepAliveRestartsTheWatchdog()
    {
        var lastWrite = new Stopwatch();
        using var server = Serve(async (c, stop) =>
        {
            await c.WriteAsync("event: connected\ndata: \"v\"\n\n");
            var first = Stopwatch.StartNew();
            while (first.Elapsed < TimeSpan.FromSeconds(3.2))
            {
                await Task.Delay(150, stop);
                lastWrite.Restart();
                await c.WriteAsync(": keep-alive\n\n");
            }

            await c.HangAsync(stop);
        });
        var total = Stopwatch.StartNew();
        using var client = Harness.Client(server, o =>
        {
            o.Stream = true;
            o.StreamWatchdog = Watchdog;
            o.PollInterval = TimeSpan.FromHours(1);
        });
        Assert.True(await client.ReadyAsync());
        await Wait.UntilAsync(() => client.Status().LastError == "timeout", "the watchdog");
        // Quiet time since the last keep-alive is the watchdog; the whole stream outlived it more than twice over.
        Assert.True(lastWrite.Elapsed >= Watchdog * 0.9, $"dropped {lastWrite.Elapsed} after the last keep-alive");
        Assert.True(total.Elapsed > Watchdog * 2, $"dropped after only {total.Elapsed}");
    }
}
