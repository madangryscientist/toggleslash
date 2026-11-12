using System.Text;
using ToggleSlash.Sdk.Json;
using ToggleSlash.Sdk.Tests.Support;
using Xunit.Abstractions;

namespace ToggleSlash.Sdk.Tests;

/// <summary>What goes over the wire: the headers of each route (privacy, section 12) and the usage body (section 9).</summary>
public class ProtocolTests(ITestOutputHelper output)
{
    // Authorization, Accept, If-None-Match and Content-Type are the SDK's; Host and Content-Length are the HTTP library's.
    private static readonly string[] Allowed = ["authorization", "accept", "if-none-match", "content-type", "host", "content-length"];

    private static readonly object Ctx = new Dictionary<string, object?> { ["key"] = "u", ["attributes"] = new Dictionary<string, object?> { ["email"] = "sentinel-attribute@example.test" } };

    [Fact]
    public async Task EveryRouteSendsOnlyTheAllowedHeadersAndNoContext()
    {
        var etag = "\"v1\"";
        var changed = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var streams = 0;
        using var server = new LoopbackServer(Rulesets.Backend(
            () => Rulesets.Ruleset(Rulesets.Flag("a", "true")),
            () => etag,
            stream: async (_, c, stop) =>
            {
                if (Interlocked.Increment(ref streams) > 1)
                {
                    await c.HangAsync(stop);
                    return;
                }

                await c.HeadAsync(200, ("Content-Type", "text/event-stream"));
                await c.WriteAsync("event: connected\ndata: \"v1\"\n\n");
                await changed.Task;
                await c.WriteAsync("event: changed\ndata: \"v2\"\n\n");
                await c.HangAsync(stop);
            }));
        using var client = Harness.Client(server, o => { o.Stream = true; o.PollInterval = TimeSpan.FromSeconds(30); });
        Assert.True(await client.ReadyAsync());
        await Wait.UntilAsync(() => client.Status().Stream == StreamState.connected, "connected");
        etag = "\"v2\"";
        changed.SetResult();
        await Wait.UntilAsync(() => client.Status().Etag == "\"v2\"", "the changed tag");
        Assert.True(client.BoolVariation("a", Ctx, false) || !client.BoolVariation("a", Ctx, true));
        await client.CloseAsync();

        var routes = new List<(string Route, Recorded Request)>();
        var rulesetSeen = 0;
        foreach (var request in server.Requests)
        {
            var route = request.Path switch
            {
                "/sdk/v1/ruleset" => request.Header("If-None-Match") is null ? "GET /sdk/v1/ruleset" : "GET /sdk/v1/ruleset (conditional)",
                "/sdk/v1/stream" => "GET /sdk/v1/stream",
                "/sdk/v1/events" => "POST /sdk/v1/events",
                _ => throw new InvalidOperationException("unexpected route " + request.Path),
            };
            routes.Add((route, request));
            rulesetSeen += request.Path == "/sdk/v1/ruleset" ? 1 : 0;
        }

        Assert.Contains(routes, r => r.Route == "GET /sdk/v1/ruleset");
        Assert.Contains(routes, r => r.Route == "GET /sdk/v1/ruleset (conditional)");
        Assert.Contains(routes, r => r.Route == "GET /sdk/v1/stream");
        Assert.Contains(routes, r => r.Route == "POST /sdk/v1/events");
        output.WriteLine("route | headers (in the order they arrived)");
        foreach (var group in routes.GroupBy(r => r.Route).OrderBy(g => g.Key, StringComparer.Ordinal))
        {
            var request = group.First().Request;
            foreach (var (name, _) in request.Headers)
            {
                Assert.Contains(name.ToLowerInvariant(), Allowed);
            }

            Assert.Equal(Harness.Key, request.Header("Authorization")?.Replace("Bearer ", string.Empty, StringComparison.Ordinal));
            Assert.DoesNotContain("sentinel-attribute", request.Body, StringComparison.Ordinal);
            Assert.DoesNotContain("\"u\"", request.Body, StringComparison.Ordinal);
            output.WriteLine($"HEADERS {group.Key} | {string.Join(", ", request.Headers.Select(h => h.Key))}");
        }
    }

    [Fact]
    public async Task TheConditionalRequestIsSentOnlyWhenARulesetAndTagAreHeld()
    {
        var calls = 0;
        using var server = new LoopbackServer(async (r, c, _) =>
        {
            if (r.Path != "/sdk/v1/ruleset")
            {
                await c.SendAsync(202, "{}");
                return;
            }

            if (Interlocked.Increment(ref calls) == 1)
            {
                await c.SendAsync(500, "no");
                return;
            }

            await (r.Header("If-None-Match") is null
                ? c.SendAsync(200, Rulesets.Ruleset(Rulesets.Flag("a", "true")), ("ETag", "\"t\""))
                : c.SendAsync(304, "", ("ETag", "\"t\"")));
        });
        using var client = Harness.Client(server);
        await Wait.UntilAsync(() => server.To("/sdk/v1/ruleset").Count >= 4, "four ruleset requests");
        var requests = server.To("/sdk/v1/ruleset");
        Assert.Null(requests[0].Header("If-None-Match"));
        Assert.Null(requests[1].Header("If-None-Match"));
        Assert.Equal("\"t\"", requests[2].Header("If-None-Match"));
        Assert.Equal("\"t\"", requests[3].Header("If-None-Match"));
        Assert.Equal(StreamState.off, client.Status().Stream);
        Assert.Null(client.Status().LastError);
    }

    [Fact]
    public async Task A304WithNoRulesetHeldIsHttp304()
    {
        using var server = new LoopbackServer(async (_, c, _) => await c.SendAsync(304, ""));
        using var client = Harness.Client(server);
        Assert.False(await client.ReadyAsync());
        Assert.Equal("http_304", client.Status().LastError);
    }

    [Fact]
    public async Task ABadRulesetBodyIsBadRuleset()
    {
        using var server = new LoopbackServer(async (_, c, _) => await c.SendAsync(200, "{\"flags\":3}", ("ETag", "\"t\"")));
        using var client = Harness.Client(server);
        Assert.False(await client.ReadyAsync());
        Assert.Equal("bad_ruleset", client.Status().LastError);
        Assert.Null(client.Status().Etag);
    }

    [Fact]
    public async Task AnUnreachableServerIsNetworkAndAHangIsTimeout()
    {
        var port = new System.Net.Sockets.TcpListener(System.Net.IPAddress.Loopback, 0);
        port.Start();
        var url = $"http://127.0.0.1:{((System.Net.IPEndPoint)port.LocalEndpoint).Port}";
        port.Stop();
        using var dead = ToggleSlashClient.Create(new ClientOptions { Url = url, SdkKey = Harness.Key, Stream = false, PollInterval = TimeSpan.FromMinutes(1) });
        Assert.False(await dead.ReadyAsync());
        Assert.Equal("network", dead.Status().LastError);

        using var hang = new LoopbackServer(async (_, c, stop) => await c.HangAsync(stop));
        using var slow = Harness.Client(hang, o => o.RequestTimeout = TimeSpan.FromMilliseconds(200));
        Assert.False(await Wait.WithinAsync(slow.ReadyAsync(), "ready"));
        Assert.Equal("timeout", slow.Status().LastError);
    }

    [Fact]
    public async Task UsageIsSplitAtUtcMidnightAndSentInChunksOf500()
    {
        var start = new DateTimeOffset(2026, 11, 10, 23, 59, 59, TimeSpan.Zero);
        var time = new CountingTimeProvider(start);
        var flags = Enumerable.Range(0, 501).Select(i => Rulesets.Flag("f" + i, "true")).ToArray();
        using var server = new LoopbackServer(Rulesets.Backend(() => Rulesets.Ruleset(flags), () => "\"v\""));
        using var client = Harness.Client(server, o =>
        {
            o.TimeProvider = time;
            o.PollInterval = TimeSpan.FromHours(1);
            o.FlushInterval = TimeSpan.FromHours(1);
        });
        Assert.True(await client.ReadyAsync());
        client.BoolVariation("f0", Ctx, false);
        time.Advance(TimeSpan.FromSeconds(2));
        client.BoolVariation("f0", Ctx, false);
        client.BoolVariation("f0", Ctx, false);
        await client.FlushAsync(false);
        var posts = server.To("/sdk/v1/events");
        Assert.Single(posts);
        var entries = Entries(posts[0]);
        Assert.Equal(2, entries.Count);
        Assert.Contains(entries, e => e.Day == "2026-11-10" && e.Count == 1);
        Assert.Contains(entries, e => e.Day == "2026-11-11" && e.Count == 2);
        Assert.All(entries, e => Assert.Equal(0, e.Index));

        for (var i = 0; i < 501; i++)
        {
            client.BoolVariation("f" + i, Ctx, false);
        }

        await client.FlushAsync(false);
        var sizes = server.To("/sdk/v1/events").Skip(1).Select(r => Entries(r).Count).OrderDescending().ToArray();
        Assert.Equal([500, 1], sizes);
    }

    [Fact]
    public async Task UsageIsKeptOnA5xxAndDroppedOnAnother4xx()
    {
        var statuses = new Queue<int>([500, 202, 400, 202]);
        using var server = new LoopbackServer(Rulesets.Backend(
            () => Rulesets.Ruleset(Rulesets.Flag("a", "true")),
            () => "\"v\"",
            events: async (_, c, _) =>
            {
                int status;
                lock (statuses)
                {
                    status = statuses.Dequeue();
                }

                await c.SendAsync(status, "{}");
            }));
        var errors = new List<Exception>();
        using var client = Harness.Client(server, o =>
        {
            o.PollInterval = TimeSpan.FromHours(1);
            o.FlushInterval = TimeSpan.FromHours(1);
            o.OnError = e => { lock (errors) { errors.Add(e); } };
        });
        Assert.True(await client.ReadyAsync());
        client.BoolVariation("a", Ctx, false);
        await client.FlushAsync(false);
        await client.FlushAsync(false);
        var posts = server.To("/sdk/v1/events");
        Assert.Equal(2, posts.Count);
        Assert.Equal(Entries(posts[0])[0].Count, Entries(posts[1])[0].Count);
        client.BoolVariation("a", Ctx, false);
        await client.FlushAsync(false);
        await client.FlushAsync(false);
        Assert.Equal(3, server.To("/sdk/v1/events").Count);
        lock (errors)
        {
            Assert.Equal(["http_500", "http_400"], errors.Select(e => ((ToggleSlashException)e).Code).ToArray());
        }
    }

    private static List<(string Flag, int Index, long Count, string Day)> Entries(Recorded request)
    {
        var body = (JsonObject)JsonValue.Parse(request.Body)!;
        var list = new List<(string, int, long, string)>();
        foreach (var item in ((JsonArray)Get(body, "events")).Items)
        {
            var o = (JsonObject)item;
            list.Add((((JsonString)Get(o, "flag")).Value, (int)((JsonNumber)Get(o, "variationIndex")).Value, (long)((JsonNumber)Get(o, "count")).Value, ((JsonString)Get(o, "day")).Value));
            Assert.Equal(4, o.Properties.Count());
        }

        Assert.Single(body.Properties);
        return list;
    }

    private static JsonValue Get(JsonObject o, string name) => o.TryGetValue(name, out var v) ? v : throw new InvalidOperationException(name);
}
