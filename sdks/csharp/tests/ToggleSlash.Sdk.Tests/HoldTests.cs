using ToggleSlash.Sdk.Tests.Support;

namespace ToggleSlash.Sdk.Tests;

/// <summary>Retry-After (7.4, 9): the cap at 300 s on all three routes, read through the client's own hold times on a fake clock.</summary>
public class HoldTests
{
    private static readonly DateTimeOffset Start = new(2026, 11, 12, 10, 0, 0, TimeSpan.Zero);
    private static readonly object Ctx = new Dictionary<string, object?> { ["key"] = "u" };

    [Theory]
    [InlineData("7", 7)]
    [InlineData(" 7 ", 7)]
    [InlineData("300", 300)]
    [InlineData("301", 300)]
    [InlineData("99999999999999999999", 300)]
    [InlineData("0", 0)]
    public void ParsesWholeDecimalSecondsAndCapsAtThreeHundred(string text, int seconds)
    {
        Assert.Equal(TimeSpan.FromSeconds(seconds), Transport.ParseRetryAfter(text));
    }

    [Theory]
    [InlineData(null)]
    [InlineData("")]
    [InlineData("1.5")]
    [InlineData("-3")]
    [InlineData("+3")]
    [InlineData("Wed, 21 Oct 2026 07:28:00 GMT")]
    [InlineData("3s")]
    [InlineData("0x10")]
    public void IgnoresAnythingThatIsNotWholeDecimalSeconds(string? text)
    {
        Assert.Null(Transport.ParseRetryAfter(text));
    }

    [Fact]
    public async Task TheRulesetRouteCapsTheHoldAtThreeHundredSeconds()
    {
        var time = new CountingTimeProvider(Start);
        using var server = new LoopbackServer(async (_, c, _) => await c.SendAsync(429, "slow down", ("Retry-After", "100000")));
        using var client = Harness.Client(server, o => o.TimeProvider = time);
        Assert.False(await client.ReadyAsync());
        Assert.Equal("http_429", client.Status().LastError);
        Assert.Equal(TimeSpan.FromSeconds(300), client.SyncHoldUntil - time.GetUtcNow());
    }

    [Fact]
    public async Task TheStreamRouteCapsTheHoldAtThreeHundredSeconds()
    {
        var time = new CountingTimeProvider(Start);
        using var server = new LoopbackServer(Rulesets.Backend(
            () => Rulesets.Ruleset(Rulesets.Flag("a", "true")),
            () => "\"v\"",
            stream: async (_, c, _) => await c.SendAsync(429, "slow down", ("Retry-After", "5000"))));
        using var client = Harness.Client(server, o =>
        {
            o.TimeProvider = time;
            o.Stream = true;
        });
        Assert.True(await client.ReadyAsync());
        await Wait.UntilAsync(() => client.Status().LastError == "http_429", "the stream's 429");
        Assert.Equal(TimeSpan.FromSeconds(300), client.SyncHoldUntil - time.GetUtcNow());
    }

    [Fact]
    public async Task TheEventsRouteCapsItsOwnHoldAndLeavesTheRulesetHoldAlone()
    {
        var time = new CountingTimeProvider(Start);
        using var server = new LoopbackServer(Rulesets.Backend(
            () => Rulesets.Ruleset(Rulesets.Flag("a", "true"), Rulesets.Flag("b", "true")),
            () => "\"v\"",
            events: async (_, c, _) => await c.SendAsync(429, "slow down", ("Retry-After", "100000"))));
        var errors = new List<Exception>();
        using var client = Harness.Client(server, o =>
        {
            o.TimeProvider = time;
            o.FlushInterval = TimeSpan.FromHours(1);
            o.PollInterval = TimeSpan.FromHours(1);
            o.OnError = e => { lock (errors) { errors.Add(e); } };
        });
        Assert.True(await client.ReadyAsync());
        client.BoolVariation("a", Ctx, false);
        await client.FlushAsync(false);
        Assert.Equal(TimeSpan.FromSeconds(300), client.UsageHoldUntil - time.GetUtcNow());
        Assert.True(client.SyncHoldUntil <= time.GetUtcNow());

        // A flush during the hold returns at once and sends nothing.
        client.BoolVariation("b", Ctx, false);
        await client.FlushAsync(false);
        Assert.Single(server.To("/sdk/v1/events"));

        // The final flush does not wait the hold out; it says how many counts it dropped.
        await client.CloseAsync();
        Assert.Single(server.To("/sdk/v1/events"));
        lock (errors)
        {
            Assert.Contains(errors, e => e.Message == "closed with 2 usage counts unsent: the server asked to wait");
        }
    }
}
