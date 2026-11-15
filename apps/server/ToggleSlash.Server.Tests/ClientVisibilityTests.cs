using System.Net;
using System.Text.Json;
using System.Text.Json.Nodes;

/// <summary>
/// A flag's <c>clientVisible</c>: a client key ships in a page, so <c>POST /sdk/v1/evaluate</c> answers it only the
/// flags marked visible to it. A server key and the admin diagnostic route answer every flag, and the ruleset does not
/// change.
/// </summary>
public sealed class ClientVisibilityTests : IDisposable
{
    private const string Flags = "/api/v1/projects/shop/flags";
    private const string Evaluate = "/sdk/v1/evaluate";
    private const string Diagnose = "/api/v1/projects/shop/environments/prod/evaluate";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    /// <summary><c>shown</c> is visible to client keys, <c>hidden</c> is not.</summary>
    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedFlag("shop", "shown", clientVisible: true);
        await server.SeedFlag("shop", "hidden");
    }

    private async Task<string> Key(string kind)
    {
        var created = await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private Task<HttpResponseMessage> Sdk(string key, object body) => server.Send(HttpMethod.Post, Evaluate, $"Bearer {key}", body);

    private static async Task<List<string>> Answered(HttpResponseMessage response)
    {
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return [.. (await ApiServer.Json(response)).GetProperty("flags").EnumerateObject().Select(p => p.Name)];
    }

    private static readonly object Context = new { key = "u1" };

    // --- the field -----------------------------------------------------------------------------------------

    [Fact]
    public async Task A_new_flag_is_hidden_unless_created_visible()
    {
        await Seed();

        Assert.False((await ApiServer.Json(await server.Get($"{Flags}/hidden"))).GetProperty("clientVisible").GetBoolean());
        Assert.True((await ApiServer.Json(await server.Get($"{Flags}/shown"))).GetProperty("clientVisible").GetBoolean());
    }

    [Fact]
    public async Task A_patch_without_the_field_leaves_it_and_one_with_it_sets_it()
    {
        await Seed();

        var renamed = await server.Patch($"{Flags}/shown", new { name = "Renamed" });
        await ApiServer.Expect(HttpStatusCode.OK, renamed);
        Assert.True((await ApiServer.Json(renamed)).GetProperty("clientVisible").GetBoolean());

        var hidden = await server.Patch($"{Flags}/shown", new { clientVisible = false });
        await ApiServer.Expect(HttpStatusCode.OK, hidden);
        Assert.False((await ApiServer.Json(hidden)).GetProperty("clientVisible").GetBoolean());
        Assert.False((await ApiServer.Json(await server.Get($"{Flags}/shown"))).GetProperty("clientVisible").GetBoolean());
    }

    [Fact]
    public async Task The_audit_trail_records_the_change()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Patch($"{Flags}/hidden", new { clientVisible = true }));

        var newest = (await ApiServer.Json(await server.Get("/api/v1/projects/shop/audit?flag=hidden"))).GetProperty("events")[0];

        Assert.Equal("updated", newest.GetProperty("action").GetString());
        Assert.False(newest.GetProperty("before").GetProperty("clientVisible").GetBoolean());
        Assert.True(newest.GetProperty("after").GetProperty("clientVisible").GetBoolean());
    }

    // --- who is answered what ------------------------------------------------------------------------------

    [Fact]
    public async Task A_client_key_is_answered_only_the_visible_flags()
    {
        await Seed();

        Assert.Equal(["shown"], await Answered(await Sdk(await Key("client"), new { context = Context })));
    }

    [Fact]
    public async Task A_server_key_and_diagnose_are_answered_every_flag()
    {
        await Seed();

        Assert.Equal(["hidden", "shown"], await Answered(await Sdk(await Key("server"), new { context = Context })));
        Assert.Equal(["hidden", "shown"], await Answered(await server.Post(Diagnose, new { context = Context })));
        Assert.Equal(["hidden"], await Answered(await server.Post(Diagnose, new { flagKey = "hidden", context = Context })));
    }

    [Fact]
    public async Task A_hidden_flag_asked_for_by_a_client_key_cannot_be_told_from_a_missing_one()
    {
        await Seed();
        var client = await Key("client");

        var hidden = await Sdk(client, new { flagKey = "hidden", context = Context });
        var missing = await Sdk(client, new { flagKey = "no-such-flag", context = Context });

        Assert.Equal(HttpStatusCode.NotFound, hidden.StatusCode);
        Assert.Equal(missing.StatusCode, hidden.StatusCode);
        Assert.Equal(await missing.Content.ReadAsStringAsync(), await hidden.Content.ReadAsStringAsync());
        Assert.Equal(["hidden"], await Answered(await Sdk(await Key("server"), new { flagKey = "hidden", context = Context })));
    }

    [Fact]
    public async Task A_visible_flag_with_a_hidden_prerequisite_still_evaluates_it()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flags}/hidden/environments/prod/toggle", new { on = true }));
        const string path = $"{Flags}/shown/environments/prod";
        var version = (await ApiServer.Json(await server.Get(path))).GetProperty("version").GetInt64();
        var config = JsonNode.Parse("""
            { "on": true, "offVariation": 1, "targets": [], "prerequisites": [{ "flagKey": "hidden", "variation": 0 }], "rules": [], "fallthrough": { "variation": 0 } }
            """)!.AsObject();
        config["version"] = version;
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put(path, JsonDocument.Parse(config.ToJsonString()).RootElement));

        var response = await Sdk(await Key("client"), new { context = Context });

        await ApiServer.Expect(HttpStatusCode.OK, response);
        var flags = (await ApiServer.Json(response)).GetProperty("flags");
        Assert.Equal(["shown"], flags.EnumerateObject().Select(p => p.Name));
        Assert.Equal("""{"value":true,"variationIndex":0,"reason":{"kind":"FALLTHROUGH"}}""", flags.GetProperty("shown").GetRawText());
    }

    [Fact]
    public async Task Marking_a_flag_visible_leaves_the_ruleset_and_its_etag_as_they_were()
    {
        await Seed();
        var serverKey = await Key("server");
        var before = await server.Send(HttpMethod.Get, "/sdk/v1/ruleset", $"Bearer {serverKey}");

        await ApiServer.Expect(HttpStatusCode.OK, await server.Patch($"{Flags}/hidden", new { clientVisible = true }));
        var after = await server.Send(HttpMethod.Get, "/sdk/v1/ruleset", $"Bearer {serverKey}");

        Assert.Equal(before.Headers.ETag, after.Headers.ETag);
        Assert.Equal(await before.Content.ReadAsStringAsync(), await after.Content.ReadAsStringAsync());
        Assert.DoesNotContain("clientVisible", await after.Content.ReadAsStringAsync(), StringComparison.OrdinalIgnoreCase);
        Assert.Equal(["hidden", "shown"], await Answered(await Sdk(await Key("client"), new { context = Context })));
    }
}
