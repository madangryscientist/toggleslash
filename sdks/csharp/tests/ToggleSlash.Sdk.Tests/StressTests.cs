using System.Globalization;
using ToggleSlash.Sdk.Tests.Support;

namespace ToggleSlash.Sdk.Tests;

/// <summary>
/// Defines the collection the stress test runs alone in. xunit runs test classes in parallel, and sixteen threads spinning
/// in a loop starve the thread pool, which delays every other test's timers (a 2 s request timeout seen at 4 s).
/// Nothing runs beside this class, so its load is its own.
/// </summary>
[CollectionDefinition(Name, DisableParallelization = true)]
public sealed class StressCollection
{
    public const string Name = "Stress";
}

/// <summary>The race detector's stand-in (the .NET runtime has none): every operation from many threads, with Close racing.</summary>
[Collection(StressCollection.Name)]
public class StressTests
{
    private static readonly object Ctx = new Dictionary<string, object?> { ["key"] = "u" };

    [Fact]
    public async Task EveryOperationFromManyThreadsWithCloseRacingNeverThrowsOrHangs()
    {
        var etag = 0;
        using var server = new LoopbackServer(Rulesets.Backend(
            () => Rulesets.Ruleset(Rulesets.Flag("a", "true", Volatile.Read(ref etag)), Rulesets.Flag("b", "\"s\""), Rulesets.Flag("c", "3")),
            () => $"\"{Interlocked.Increment(ref etag).ToString(CultureInfo.InvariantCulture)}\"",
            stream: async (_, c, stop) =>
            {
                await c.HeadAsync(200, ("Content-Type", "text/event-stream"));
                await c.WriteAsync("event: connected\ndata: \"0\"\n\n");
                for (var i = 0; !stop.IsCancellationRequested; i++)
                {
                    await Task.Delay(3, stop);
                    await c.WriteAsync($"event: changed\ndata: \"c{i.ToString(CultureInfo.InvariantCulture)}\"\n\n");
                }
            }));
        for (var round = 0; round < 3; round++)
        {
            var failures = new List<Exception>();
            var client = Harness.Client(server, o =>
            {
                o.Stream = true;
                o.PollInterval = TimeSpan.FromMilliseconds(5);
                o.FlushInterval = TimeSpan.FromMilliseconds(5);
                o.AfterEvaluation = _ => { };
                o.OnError = _ => { };
            });
            var stop = new CancellationTokenSource();
            var workers = Enumerable.Range(0, 16).Select(t => Task.Run(() =>
            {
                try
                {
                    var i = t;
                    while (!stop.IsCancellationRequested)
                    {
                        client.BoolVariation("a", Ctx, false);
                        client.StringVariationDetail("b", Ctx, "d");
                        client.IntVariation("c", Ctx, 0);
                        client.FloatVariation("c", Ctx, 0);
                        client.JsonVariation("b", Ctx, null);
                        client.Variation<int?>("c", Ctx, null);
                        _ = client.Status();
                        _ = client.Ready();
                        _ = client.ReadyAsync().IsCompleted;
                        if (++i % 97 == 0)
                        {
                            _ = client.FlushAsync(false);
                        }
                    }
                }
                catch (Exception e)
                {
                    lock (failures)
                    {
                        failures.Add(e);
                    }
                }
            })).ToArray();

            await Task.Delay(300);
            var closers = new[] { Task.Run(client.Close), Task.Run(() => client.CloseAsync()), Task.Run(client.Dispose) };
            await Wait.WithinAsync(Task.WhenAll(closers), "two or three closes racing", 60_000);
            await Task.Delay(100);
            stop.Cancel();
            await Wait.WithinAsync(Task.WhenAll(workers), "workers", 60_000);
            lock (failures)
            {
                Assert.Empty(failures);
            }

            Assert.Equal(StreamState.closed, client.Status().Stream);
        }
    }
}
