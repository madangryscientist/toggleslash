using System.Net;
using System.Text.Json;

/// <summary>
/// <c>GET /api/v1/projects/{project}/environments/{env}/runtime</c>: the tag and flag count of the ruleset the server
/// would serve, and the last write that could have changed it. Nothing about what an SDK has loaded.
/// </summary>
public sealed class RuntimeApiTests : IDisposable
{
    private const string Prod = "/api/v1/projects/shop/environments/prod/runtime";
    private const string Staging = "/api/v1/projects/shop/environments/staging/runtime";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private async Task Seed(params string[] flags)
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("shop", "staging");
        foreach (var flag in flags)
        {
            await server.SeedFlag("shop", flag);
        }
    }

    private Task<HttpResponseMessage> Toggle(string flag, string env, bool on, string? authorization = null) =>
        server.Send(HttpMethod.Post, $"/api/v1/projects/shop/flags/{flag}/environments/{env}/toggle", authorization ?? $"Bearer {ApiServer.AdminToken}", new { on });

    private async Task<JsonElement> Runtime(string path = Prod)
    {
        var response = await server.Get(path);
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return await ApiServer.Json(response);
    }

    private async Task<JsonElement?> LastChange(string path = Prod)
    {
        var last = (await Runtime(path)).GetProperty("lastChange");
        return last.ValueKind == JsonValueKind.Null ? null : last;
    }

    private async Task<string> Key(string kind, string env = "prod")
    {
        var created = await server.Post($"/api/v1/projects/shop/environments/{env}/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private async Task<string> RulesetETag(string env = "prod")
    {
        var response = await server.Send(HttpMethod.Get, "/sdk/v1/ruleset", $"Bearer {await Key("server", env)}");
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return response.Headers.ETag!.ToString();
    }

    private static async Task ExpectChange(Task<JsonElement?> read, string entityType, string entityId, string action, string actor = "admin")
    {
        var last = await read;
        Assert.NotNull(last);
        Assert.Equal(entityType, last.Value.GetProperty("entityType").GetString());
        Assert.Equal(entityId, last.Value.GetProperty("entityId").GetString());
        Assert.Equal(action, last.Value.GetProperty("action").GetString());
        Assert.Equal(actor, last.Value.GetProperty("actor").GetString());
    }

    [Fact]
    public async Task The_etag_is_the_one_the_ruleset_route_gives_a_server_key_before_and_after_a_change()
    {
        await Seed("checkout");

        Assert.Equal(await RulesetETag(), (await Runtime()).GetProperty("etag").GetString());

        await ApiServer.Expect(HttpStatusCode.OK, await Toggle("checkout", "prod", true));

        var after = (await Runtime()).GetProperty("etag").GetString();
        Assert.Equal(await RulesetETag(), after);
        Assert.StartsWith("\"", after);
        Assert.EndsWith("\"", after);
    }

    [Fact]
    public async Task Two_environments_whose_rules_differ_have_different_etags()
    {
        await Seed("checkout");
        await ApiServer.Expect(HttpStatusCode.OK, await Toggle("checkout", "prod", true));

        var prod = (await Runtime(Prod)).GetProperty("etag").GetString();
        var staging = (await Runtime(Staging)).GetProperty("etag").GetString();

        Assert.NotEqual(prod, staging);
        Assert.Equal(await RulesetETag("staging"), staging);
    }

    [Fact]
    public async Task The_flag_count_is_the_ruleset_and_drops_when_a_flag_is_archived()
    {
        await Seed("checkout", "search", "old");
        Assert.Equal(3, (await Runtime()).GetProperty("flagCount").GetInt32());

        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/old/archive"));

        Assert.Equal(2, (await Runtime()).GetProperty("flagCount").GetInt32());
    }

    [Fact]
    public async Task The_answer_has_exactly_these_fields()
    {
        await Seed("checkout");
        await ApiServer.Expect(HttpStatusCode.OK, await Toggle("checkout", "prod", true));
        var body = JsonDocument.Parse(await (await server.Get(Prod)).Content.ReadAsStringAsync()).RootElement;

        Assert.Equal(["etag", "flagCount", "generatedAt", "lastChange"], body.EnumerateObject().Select(p => p.Name).Order().ToArray());
        Assert.Equal(
            ["action", "actor", "at", "entityId", "entityType"],
            body.GetProperty("lastChange").EnumerateObject().Select(p => p.Name).Order().ToArray());
        Assert.True(DateTime.UtcNow - body.GetProperty("generatedAt").GetDateTime().ToUniversalTime() < TimeSpan.FromMinutes(1));
        Assert.EndsWith("Z", body.GetProperty("generatedAt").GetString());
        Assert.EndsWith("Z", body.GetProperty("lastChange").GetProperty("at").GetString());
    }

    [Fact]
    public async Task A_toggle_in_another_environment_does_not()
    {
        await Seed("checkout");
        await ApiServer.Expect(HttpStatusCode.OK, await Toggle("checkout", "prod", true));
        await ApiServer.Expect(HttpStatusCode.OK, await Toggle("checkout", "staging", true));

        await ExpectChange(LastChange(Prod), "flag_config", "checkout/prod", "toggled");
        await ExpectChange(LastChange(Staging), "flag_config", "checkout/staging", "toggled");
    }

    [Fact]
    public async Task A_segment_change_counts_here_and_not_in_another_environment()
    {
        await Seed();
        await ApiServer.Expect(
            HttpStatusCode.Created,
            await server.Post("/api/v1/projects/shop/environments/staging/segments", new { key = "beta", included = new string[0], excluded = new string[0], rules = new object[0] }));

        Assert.Null(await LastChange(Prod));
        await ExpectChange(LastChange(Staging), "segment", "beta", "created");
    }

    [Fact]
    public async Task A_flag_created_archived_or_restored_in_this_project_counts_and_one_in_another_project_does_not()
    {
        await Seed();
        await server.SeedProject("other");
        await server.SeedFlag("other", "elsewhere");
        Assert.Null(await LastChange());

        await server.SeedFlag("shop", "checkout");
        await ExpectChange(LastChange(), "flag", "checkout", "created");

        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/checkout/archive"));
        await ExpectChange(LastChange(), "flag", "checkout", "archived");

        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/checkout/restore"));
        await ExpectChange(LastChange(), "flag", "checkout", "restored");
    }

    [Fact]
    public async Task Editing_a_flags_metadata_does_not_count_because_the_ruleset_does_not_carry_it()
    {
        await Seed("checkout");
        var etag = (await Runtime()).GetProperty("etag").GetString();

        await ApiServer.Expect(HttpStatusCode.OK, await server.Patch("/api/v1/projects/shop/flags/checkout", new { name = "Checkout", tags = new[] { "payments" } }));

        await ExpectChange(LastChange(), "flag", "checkout", "created");
        Assert.Equal(etag, (await Runtime()).GetProperty("etag").GetString());
    }

    [Fact]
    public async Task Keys_tokens_projects_and_environments_do_not_count_and_an_environment_with_nothing_counted_is_null()
    {
        await Seed();
        await Key("server");
        await Key("client");
        var keys = await ApiServer.Json(await server.Get("/api/v1/projects/shop/environments/prod/keys"));
        var id = keys.EnumerateArray().First().GetProperty("id").GetInt64();
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"/api/v1/projects/shop/environments/prod/keys/{id}"));
        var minted = await server.Post("/api/v1/projects/shop/tokens", new { name = "ci", permission = "write", expiresInDays = 30 });
        await ApiServer.Expect(HttpStatusCode.Created, minted);
        var tokenId = (await ApiServer.Json(minted)).GetProperty("id").GetInt64();
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"/api/v1/projects/shop/tokens/{tokenId}"));

        Assert.Equal(["admin_token", "api_key", "environment", "project"], server.AuditRows().Select(r => r.EntityType).Distinct().Order().ToArray());
        Assert.Null(await LastChange());
    }

    [Fact]
    public async Task A_no_op_write_does_not_count()
    {
        await Seed("checkout");
        await ApiServer.Expect(HttpStatusCode.OK, await Toggle("checkout", "prod", true));
        // Restoring a flag that is not archived changes nothing but still records an event, with before equal to after.
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/checkout/restore"));
        var rows = server.AuditRows();
        Assert.Equal("restored", rows[^1].Action);
        Assert.Equal(rows[^1].Before, rows[^1].After);

        await ExpectChange(LastChange(), "flag_config", "checkout/prod", "toggled");
    }

    [Fact]
    public async Task A_toggle_to_the_value_it_has_still_counts_because_it_bumps_the_version_the_ruleset_carries()
    {
        await Seed("checkout");
        await ApiServer.Expect(HttpStatusCode.OK, await Toggle("checkout", "prod", false));
        var etag = (await Runtime()).GetProperty("etag").GetString();

        await ApiServer.Expect(HttpStatusCode.OK, await Toggle("checkout", "prod", false));

        await ExpectChange(LastChange(), "flag_config", "checkout/prod", "toggled");
        Assert.NotEqual(etag, (await Runtime()).GetProperty("etag").GetString());
    }

    [Fact]
    public async Task A_write_through_a_scoped_token_shows_the_token_as_the_actor()
    {
        await Seed("checkout");
        var minted = await server.Post("/api/v1/projects/shop/tokens", new { name = "ci", permission = "write", expiresInDays = 30 });
        await ApiServer.Expect(HttpStatusCode.Created, minted);
        var body = await ApiServer.Json(minted);

        await ApiServer.Expect(HttpStatusCode.OK, await Toggle("checkout", "prod", true, $"Bearer {body.GetProperty("token").GetString()}"));

        await ExpectChange(LastChange(), "flag_config", "checkout/prod", "toggled", $"token:{body.GetProperty("id").GetInt64()}");
    }

    [Theory]
    [InlineData("nope", "prod")]
    [InlineData("shop", "nope")]
    public async Task An_unknown_project_or_environment_is_404(string project, string env)
    {
        await Seed();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"/api/v1/projects/{project}/environments/{env}/runtime"));
    }

    [Fact]
    public async Task No_token_is_401_and_an_sdk_key_is_403()
    {
        await Seed();

        await ApiServer.Expect(HttpStatusCode.Unauthorized, await server.Send(HttpMethod.Get, Prod, null));
        await ApiServer.Expect(HttpStatusCode.Forbidden, await server.Send(HttpMethod.Get, Prod, $"Bearer {await Key("server")}"));
        await ApiServer.Expect(HttpStatusCode.Forbidden, await server.Send(HttpMethod.Get, Prod, $"Bearer {await Key("client")}"));
    }

    [Fact]
    public async Task Reading_it_records_no_audit_event()
    {
        await Seed("checkout");
        var before = server.AuditRows().Count;

        await Runtime();

        Assert.Equal(before, server.AuditRows().Count);
    }
}
