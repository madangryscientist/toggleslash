using System.Text.Json;
using Microsoft.Extensions.Time.Testing;

namespace ToggleSlash.Sdk.Tests;

/// <summary>Long timers, through a fake clock and a fake HTTP handler: no test here waits a real minute.</summary>
public sealed class TimerTests
{
    private static readonly Context User = Context.ForKey("u1");
    private static readonly DateTimeOffset Noon = new(2026, 11, 10, 12, 0, 0, TimeSpan.Zero);
    private static readonly TimeSpan Cap = TimeSpan.FromSeconds(300);

    private static Task<HttpResponseMessage> Ordinary(Seen r) =>
        r.Path switch
        {
            Reply.Ruleset when r.Headers.ContainsKey("If-None-Match") => Reply.Status(304),
            Reply.Ruleset => Reply.Rules(Flags.Constant("f", "true"), "\"v1\""),
            Reply.Stream => Reply.Status(500),
            _ => Reply.Status(202),
        };

    [Fact]
    public async Task A_retry_after_above_300_holds_the_ruleset_route_for_300_seconds()
    {
        var time = new Clock(Noon);
        var calls = 0;
        var server = new FakeServer(time, (r, _) => Interlocked.Increment(ref calls) == 1 ? Reply.Status(429, "100000") : Ordinary(r));
        var client = ToggleSlashClient.Create(new ClientOptions { Url = "http://sdk.invalid", SdkKey = "key", Stream = false, Time = time, Handler = server });
        Assert.False(await client.ReadyAsync());
        Assert.Equal("http_429", client.Status.LastError);
        var first = Assert.Single(server.To(Reply.Ruleset));

        await Wait.Advance(time, TimeSpan.FromSeconds(299), TimeSpan.FromSeconds(10), timers: 2);
        Assert.Single(server.To(Reply.Ruleset));
        await Wait.Advance(time, TimeSpan.FromSeconds(6), TimeSpan.FromSeconds(1), timers: 2);
        await Wait.Until(() => server.To(Reply.Ruleset).Count == 2, "the second ruleset request");
        var gap = server.To(Reply.Ruleset)[1].At - first.At;
        Assert.InRange(gap, Cap, Cap + TimeSpan.FromSeconds(6));
        await client.CloseAsync();
    }

    [Fact]
    public async Task A_retry_after_above_300_holds_the_stream_and_the_ruleset_for_300_seconds()
    {
        var time = new Clock(Noon);
        var streams = 0;
        var server = new FakeServer(time, (r, _) => r.Path == Reply.Stream && Interlocked.Increment(ref streams) == 1 ? Reply.Status(429, "100000") : Ordinary(r));
        var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = "http://sdk.invalid",
            SdkKey = "key",
            Time = time,
            Jitter = () => 0,
            Handler = server,
        });
        Assert.True(await client.ReadyAsync());
        await Wait.Until(() => server.To(Reply.Stream).Count == 1, "the first stream request");
        var refused = server.To(Reply.Stream)[0];
        await Wait.Until(() => client.Status.LastError == "http_429", "the 429 to be recorded");
        var before = server.Requests.Count;

        // Neither route is asked for anything until 300 s have passed, though the backoff alone is a second.
        await Wait.Advance(time, TimeSpan.FromSeconds(299), TimeSpan.FromSeconds(10));
        Assert.Equal(before, server.Requests.Count);
        await Wait.Advance(time, TimeSpan.FromSeconds(6));
        await Wait.Until(() => server.Requests.Count > before, "a request after the hold");
        Assert.True(server.Requests.Skip(before).All(r => r.At - refused.At >= Cap), "a request left before the hold ended");
        await client.CloseAsync();
    }

    [Fact]
    public async Task A_retry_after_above_300_holds_usage_reports_for_300_seconds_and_keeps_the_counts()
    {
        var time = new Clock(Noon);
        var posts = 0;
        var server = new FakeServer(time, (r, _) => r.Path == Reply.Events && Interlocked.Increment(ref posts) == 1 ? Reply.Status(429, "100000") : Ordinary(r));
        var client = ToggleSlashClient.Create(new ClientOptions { Url = "http://sdk.invalid", SdkKey = "key", Stream = false, Time = time, Handler = server });
        Assert.True(await client.ReadyAsync());
        client.BoolVariation("f", User, false);

        await Wait.Advance(time, TimeSpan.FromSeconds(30), TimeSpan.FromSeconds(1), timers: 2);
        await Wait.Until(() => server.To(Reply.Events).Count == 1, "the first usage report");
        var first = server.To(Reply.Events)[0];
        await Wait.Advance(time, TimeSpan.FromSeconds(290), TimeSpan.FromSeconds(10), timers: 2);
        Assert.Single(server.To(Reply.Events));
        await Wait.Advance(time, TimeSpan.FromSeconds(40), TimeSpan.FromSeconds(5), timers: 2);
        await Wait.Until(() => server.To(Reply.Events).Count == 2, "the report after the hold");
        var second = server.To(Reply.Events)[1];
        Assert.InRange(second.At - first.At, Cap, Cap + TimeSpan.FromSeconds(35));
        using var body = JsonDocument.Parse(second.Body);
        var entry = Assert.Single(body.RootElement.GetProperty("events").EnumerateArray().ToList());
        Assert.Equal("f", entry.GetProperty("flag").GetString());
        Assert.Equal(1, entry.GetProperty("count").GetInt64());
        await client.CloseAsync();
    }

    [Fact]
    public async Task A_retry_after_under_the_cap_is_held_as_given_and_a_date_is_ignored()
    {
        var time = new Clock(Noon);
        var calls = 0;
        var server = new FakeServer(time, (r, _) => Interlocked.Increment(ref calls) switch
        {
            1 => Reply.Status(429, "120"),
            2 => Reply.Status(429, "Wed, 21 Oct 2026 07:28:00 GMT"),
            _ => Ordinary(r),
        });
        var client = ToggleSlashClient.Create(new ClientOptions { Url = "http://sdk.invalid", SdkKey = "key", Stream = false, Time = time, Handler = server });
        Assert.False(await client.ReadyAsync());
        await Wait.Advance(time, TimeSpan.FromSeconds(119), TimeSpan.FromSeconds(10), timers: 2);
        Assert.Single(server.To(Reply.Ruleset));
        await Wait.Advance(time, TimeSpan.FromSeconds(3), TimeSpan.FromSeconds(1), timers: 2);
        await Wait.Until(() => server.To(Reply.Ruleset).Count == 2, "the retry after 120 s");
        // The date is no wait at all, so the ordinary poll interval (30 s) rules.
        await Wait.Advance(time, TimeSpan.FromSeconds(31), TimeSpan.FromSeconds(1), timers: 2);
        await Wait.Until(() => server.To(Reply.Ruleset).Count == 3, "the poll after a date-valued Retry-After");
        await client.CloseAsync();
    }

    [Fact]
    public async Task Counts_either_side_of_a_utc_midnight_go_out_as_two_days()
    {
        var time = new Clock(new DateTimeOffset(2026, 11, 10, 23, 59, 59, TimeSpan.Zero));
        var server = new FakeServer(time, (r, _) => Ordinary(r));
        var client = ToggleSlashClient.Create(new ClientOptions { Url = "http://sdk.invalid", SdkKey = "key", Stream = false, Time = time, Handler = server });
        Assert.True(await client.ReadyAsync());
        client.BoolVariation("f", User, false);
        client.BoolVariation("f", User, false);
        await Wait.Advance(time, TimeSpan.FromSeconds(2), TimeSpan.FromSeconds(1), timers: 2);
        client.BoolVariation("f", User, false);
        await Wait.Advance(time, TimeSpan.FromSeconds(30), TimeSpan.FromSeconds(1), timers: 2);
        await Wait.Until(() => server.To(Reply.Events).Count == 1, "the usage report");
        using var body = JsonDocument.Parse(server.To(Reply.Events)[0].Body);
        var days = body.RootElement.GetProperty("events").EnumerateArray()
            .ToDictionary(e => e.GetProperty("day").GetString()!, e => e.GetProperty("count").GetInt64());
        Assert.Equal(new Dictionary<string, long> { ["2026-11-10"] = 2, ["2026-11-11"] = 1 }, days);
        await client.CloseAsync();
    }

    [Fact]
    public async Task More_than_500_entries_go_out_in_batches_of_at_most_500()
    {
        var time = new Clock(Noon);
        var flags = string.Join(",", Enumerable.Range(0, 1203).Select(i => Flags.Constant("flag-" + i, "true")));
        var server = new FakeServer(time, (r, _) => r.Path == Reply.Ruleset ? Reply.Rules(flags, "\"v1\"") : Ordinary(r));
        var client = ToggleSlashClient.Create(new ClientOptions { Url = "http://sdk.invalid", SdkKey = "key", Stream = false, Time = time, Handler = server });
        Assert.True(await client.ReadyAsync());
        for (var i = 0; i < 1203; i++) client.BoolVariation("flag-" + i, User, false);
        await client.CloseAsync();
        var sizes = server.To(Reply.Events).Select(r => JsonDocument.Parse(r.Body).RootElement.GetProperty("events").GetArrayLength()).ToList();
        Assert.Equal([500, 500, 203], sizes);
    }
}
