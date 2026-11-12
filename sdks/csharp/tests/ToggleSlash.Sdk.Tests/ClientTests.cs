using ToggleSlash.Sdk.Json;
using ToggleSlash.Sdk.Tests.Support;

namespace ToggleSlash.Sdk.Tests;

public class ClientTests
{
    private static readonly object Ctx = new Dictionary<string, object?> { ["key"] = "u" };

    private static LoopbackServer Serve(string ruleset, string etag = "\"v1\"") =>
        new(Rulesets.Backend(() => ruleset, () => etag));

    [Fact]
    public async Task ServesTheFallbackWithNotReadyUntilARulesetIsHeld()
    {
        using var server = new LoopbackServer(async (_, c, stop) => await c.HangAsync(stop));
        using var client = Harness.Client(server);
        var detail = client.BoolVariationDetail("a", Ctx, true);
        Assert.True(detail.Value);
        Assert.Null(detail.VariationIndex);
        Assert.Equal(ErrorCode.NOT_READY, detail.Reason.ErrorCode);
        Assert.Equal(ReasonKind.ERROR, detail.Reason.Kind);
    }

    [Fact]
    public async Task ReadyIsTrueAfterTheFirstLoadAndFalseWhenItFails()
    {
        using var good = Serve(Rulesets.Ruleset(Rulesets.Flag("a", "true")));
        using var ok = Harness.Client(good);
        Assert.True(await Wait.WithinAsync(ok.ReadyAsync(), "ready"));
        Assert.True(ok.Ready());
        Assert.True(ok.BoolVariation("a", Ctx, false) || !ok.BoolVariation("a", Ctx, true));

        using var bad = new LoopbackServer(async (_, c, _) => await c.SendAsync(500, "x"));
        using var broken = Harness.Client(bad);
        Assert.False(await Wait.WithinAsync(broken.ReadyAsync(), "ready"));
        Assert.Equal("http_500", broken.Status().LastError);
    }

    [Fact]
    public async Task EveryAccessorServesItsKind()
    {
        var ruleset = Rulesets.Ruleset(
            Rulesets.Flag("b", "true"),
            Rulesets.Flag("s", "\"x\""),
            Rulesets.Flag("i", "7"),
            Rulesets.Flag("f", "1.5"),
            Rulesets.Flag("j", "{\"k\":[1]}"));
        using var server = Serve(ruleset);
        using var client = Harness.Client(server);
        Assert.True(await client.ReadyAsync());
        Assert.True(client.BoolVariation("b", Ctx, false));
        Assert.Equal("x", client.StringVariation("s", Ctx, "d"));
        Assert.Equal(7, client.IntVariation("i", Ctx, 0));
        Assert.Equal(1.5, client.FloatVariation("f", Ctx, 0));
        Assert.Equal("{\"k\":[1]}", client.JsonVariation("j", Ctx, Harness.Json("{}"))!.ToJson());
        var mismatch = client.StringVariationDetail("b", Ctx, "d");
        Assert.Equal("d", mismatch.Value);
        Assert.Null(mismatch.VariationIndex);
        Assert.Equal(ErrorCode.TYPE_MISMATCH, mismatch.Reason.ErrorCode);
        var missing = client.BoolVariationDetail("nope", Ctx, true);
        Assert.True(missing.Value);
        Assert.Equal(ErrorCode.FLAG_NOT_FOUND, missing.Reason.ErrorCode);
    }

    [Fact]
    public async Task IntegerAccessorAcceptsAWholeFloatAndRefusesAFractionOrARangeOverflow()
    {
        var ruleset = Rulesets.Ruleset(
            Rulesets.Flag("two", "2.0"),
            Rulesets.Flag("frac", "2.5"),
            Rulesets.Flag("big", "9223372036854775808"),
            Rulesets.Flag("huge", "1e30"),
            Rulesets.Flag("low", "-9223372036854775808"));
        using var server = Serve(ruleset);
        using var client = Harness.Client(server);
        Assert.True(await client.ReadyAsync());
        Assert.Equal(2, client.IntVariationDetail("two", Ctx, 0).Value);
        Assert.Equal(long.MinValue, client.IntVariationDetail("low", Ctx, 0).Value);
        foreach (var key in new[] { "frac", "big", "huge" })
        {
            var detail = client.IntVariationDetail(key, Ctx, 99);
            Assert.Equal(99, detail.Value);
            Assert.Equal(ErrorCode.TYPE_MISMATCH, detail.Reason.ErrorCode);
        }
    }

    [Fact]
    public async Task NullableStringAccessorWithANullFallbackRefusesAnotherKind()
    {
        using var server = Serve(Rulesets.Ruleset(Rulesets.Flag("n", "5"), Rulesets.Flag("s", "\"x\"")));
        using var client = Harness.Client(server);
        Assert.True(await client.ReadyAsync());
        var mismatch = client.StringVariationDetail("n", Ctx, null);
        Assert.Null(mismatch.Value);
        Assert.Equal(ErrorCode.TYPE_MISMATCH, mismatch.Reason.ErrorCode);
        Assert.Equal("x", client.StringVariationDetail("s", Ctx, null).Value);
        var generic = client.VariationDetail<string?>("n", Ctx, null);
        Assert.Null(generic.Value);
        Assert.Equal(ErrorCode.TYPE_MISMATCH, generic.Reason.ErrorCode);
    }

    [Fact]
    public async Task GenericAccessorReadsIntegerTypesAndRefusesATypeWithNoJsonKind()
    {
        var ruleset = Rulesets.Ruleset(
            Rulesets.Flag("two", "2.0"),
            Rulesets.Flag("frac", "2.5"),
            Rulesets.Flag("big", "3000000000"),
            Rulesets.Flag("s", "\"x\""));
        using var server = Serve(ruleset);
        using var client = Harness.Client(server);
        Assert.True(await client.ReadyAsync());
        Assert.Equal(2, client.Variation("two", Ctx, 0));
        Assert.Equal((short)2, client.Variation("two", Ctx, (short)0));
        var fraction = client.VariationDetail("frac", Ctx, 5);
        Assert.Equal(5, fraction.Value);
        Assert.Equal(ErrorCode.TYPE_MISMATCH, fraction.Reason.ErrorCode);
        var overflow = client.VariationDetail("big", Ctx, 5);
        Assert.Equal(5, overflow.Value);
        Assert.Equal(ErrorCode.TYPE_MISMATCH, overflow.Reason.ErrorCode);
        Assert.Equal(3_000_000_000L, client.Variation("big", Ctx, 0L));
        var none = client.VariationDetail("s", Ctx, new List<int>());
        Assert.Empty(none.Value);
        Assert.Equal(ErrorCode.TYPE_MISMATCH, none.Reason.ErrorCode);
        Assert.Null(none.VariationIndex);
    }

    [Fact]
    public async Task HookValueForTheIntegerAccessorIsALong()
    {
        using var server = Serve(Rulesets.Ruleset(Rulesets.Flag("i", "7")));
        var events = new List<EvaluationEvent>();
        using var client = Harness.Client(server, o => o.AfterEvaluation = e => { lock (events) { events.Add(e); } });
        Assert.True(await client.ReadyAsync());
        client.IntVariation("i", Ctx, 0);
        client.BoolVariation("i", Ctx, false);
        lock (events)
        {
            Assert.Equal(2, events.Count);
            Assert.IsType<long>(events[0].Value);
            Assert.Equal(7L, events[0].Value);
            Assert.Equal(0, events[0].VariationIndex);
            Assert.Equal("\"v1\"", events[0].Etag);
            Assert.Equal(1L, events[0].FlagVersion);
            Assert.IsType<bool>(events[1].Value);
            Assert.Null(events[1].VariationIndex);
        }
    }

    [Fact]
    public async Task ADeadEvaluatorGivesTheFallbackWithOneOnErrorAndNoCount()
    {
        using var server = Serve(Rulesets.Ruleset(Rulesets.Flag("a", "true")));
        var errors = new List<Exception>();
        using var client = Harness.Client(server, o =>
        {
            o.Evaluator = (_, _, _) => throw new InvalidOperationException("boom");
            o.OnError = e => { lock (errors) { errors.Add(e); } };
            o.FlushInterval = TimeSpan.FromMilliseconds(20);
        });
        Assert.True(await client.ReadyAsync());
        var detail = client.BoolVariationDetail("a", Ctx, false);
        Assert.False(detail.Value);
        Assert.Null(detail.VariationIndex);
        Assert.Equal(ErrorCode.EVALUATION_FAILED, detail.Reason.ErrorCode);
        lock (errors)
        {
            Assert.Single(errors);
        }

        await client.CloseAsync();
        Assert.Empty(server.To("/sdk/v1/events"));
    }

    [Fact]
    public async Task AnUnusableContextValueGivesInvalidContext()
    {
        using var server = Serve(Rulesets.Ruleset(Rulesets.Flag("a", "true")));
        using var client = Harness.Client(server);
        Assert.True(await client.ReadyAsync());
        var detail = client.BoolVariationDetail("a", new Dictionary<string, object?> { ["name"] = "no key" }, false);
        Assert.False(detail.Value);
        Assert.Equal(ErrorCode.INVALID_CONTEXT, detail.Reason.ErrorCode);
    }

    [Fact]
    public async Task AFlagThatIsOnAndHasNoOtherDataIsServedByTheSameJsonParserTheServiceUses()
    {
        using var server = Serve(Rulesets.Ruleset(Rulesets.Flag("a", "\"\\ud800\"")));
        using var client = Harness.Client(server);
        Assert.True(await client.ReadyAsync());
        Assert.IsType<JsonString>(client.JsonVariation("a", Ctx, Harness.Json("\"\"")));
    }
}
