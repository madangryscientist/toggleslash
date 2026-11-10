using System.Text.Json;
using Microsoft.Extensions.Time.Testing;

namespace ToggleSlash.Sdk.Tests;

/// <summary>Contract 4, "Typed languages": what the integer accessor takes.</summary>
public sealed class AccessorTests
{
    private static readonly Context User = Context.ForKey("u1");

    private static async Task<ToggleSlashClient> With(params (string Key, string Value)[] flags)
    {
        var server = new FakeServer(TimeProvider.System, (r, _) =>
            r.Path == Reply.Ruleset ? Reply.Rules(string.Join(",", flags.Select(f => Flags.Constant(f.Key, f.Value))), "\"v1\"") : Reply.Status(202));
        var client = ToggleSlashClient.Create(new ClientOptions { Url = "http://sdk.invalid", SdkKey = "key", Stream = false, Handler = server });
        Assert.True(await client.ReadyAsync());
        return client;
    }

    [Theory]
    [InlineData("2", 2L)]
    [InlineData("2.0", 2L)]
    [InlineData("-0", 0L)]
    [InlineData("-9223372036854775808", long.MinValue)]
    [InlineData("1e3", 1000L)]
    public async Task A_whole_number_in_signed_64_bits_is_an_integer(string json, long expected)
    {
        using var client = await With(("n", json));
        var detail = client.IntVariationDetail("n", User, -1);
        Assert.Equal(expected, detail.Value);
        Assert.Equal(ReasonKind.Fallthrough, detail.Reason.Kind);
    }

    [Theory]
    [InlineData("2.5")]
    [InlineData("1e20")]
    [InlineData("9223372036854775807")]
    [InlineData("9223372036854775808")]
    [InlineData("-9223372036854777856")]
    [InlineData("\"2\"")]
    [InlineData("true")]
    [InlineData("null")]
    public async Task A_fraction_a_number_outside_64_bits_or_another_kind_is_a_type_mismatch(string json)
    {
        using var client = await With(("n", json));
        var detail = client.IntVariationDetail("n", User, -1);
        Assert.Equal(-1, detail.Value);
        Assert.Null(detail.VariationIndex);
        Assert.Equal(ReasonKind.Error, detail.Reason.Kind);
        Assert.Equal(ErrorCode.TypeMismatch, detail.Reason.Code);
    }

    [Fact]
    public async Task A_null_json_fallback_accepts_any_kind_and_another_kind_is_a_mismatch()
    {
        using var client = await With(("s", "\"x\""), ("o", "{\"a\":1}"));
        Assert.Equal("x", client.JsonVariation("s", User, default).GetString());
        Assert.Equal(1, client.JsonVariation("o", User, default).GetProperty("a").GetInt32());
        var wrong = client.JsonVariationDetail("s", User, JsonDocument.Parse("[]").RootElement);
        Assert.Equal(ErrorCode.TypeMismatch, wrong.Reason.Code);
    }

    [Fact]
    public async Task A_duplicate_key_in_the_ruleset_keeps_the_last_value()
    {
        var body = "{\"flags\":[{\"key\":\"f\",\"variations\":[1,2],\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],\"fallthrough\":{\"variation\":0,\"variation\":1},\"salt\":\"s\",\"version\":1}],\"segments\":[]}";
        var server = new FakeServer(TimeProvider.System, (r, _) => r.Path == Reply.Ruleset
            ? Task.FromResult(new HttpResponseMessage(System.Net.HttpStatusCode.OK) { Content = new StringContent(body) })
            : Reply.Status(202));
        using var client = ToggleSlashClient.Create(new ClientOptions { Url = "http://sdk.invalid", SdkKey = "key", Stream = false, Handler = server });
        Assert.True(await client.ReadyAsync());
        Assert.Equal(2, client.IntVariation("f", User, 0));
    }

    [Fact]
    public async Task A_body_that_is_not_a_ruleset_never_replaces_the_one_held()
    {
        var time = new FakeTimeProvider(new DateTimeOffset(2026, 11, 10, 12, 0, 0, TimeSpan.Zero));
        var calls = 0;
        var server = new FakeServer(time, (r, _) => r.Path switch
        {
            Reply.Ruleset when Interlocked.Increment(ref calls) == 1 => Reply.Rules(Flags.Constant("f", "7"), "\"v1\""),
            Reply.Ruleset => Task.FromResult(new HttpResponseMessage(System.Net.HttpStatusCode.OK) { Content = new StringContent("{\"flags\":{}}") }),
            _ => Reply.Status(202),
        });
        using var client = ToggleSlashClient.Create(new ClientOptions { Url = "http://sdk.invalid", SdkKey = "key", Stream = false, Time = time, Handler = server });
        Assert.True(await client.ReadyAsync());
        await Wait.Advance(time, TimeSpan.FromSeconds(31));
        await Wait.Until(() => client.Status.LastError == "bad_ruleset", "bad_ruleset");
        Assert.Equal("\"v1\"", client.Status.Etag);
        Assert.Equal(7, client.IntVariation("f", User, 0));
    }
}
