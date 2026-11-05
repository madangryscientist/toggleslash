using System.Net;
using System.Text.Json;

/// <summary><c>GET .../environments/{env}/flag-states</c>: every flag's on/off, version and last usage day in one environment, in one read.</summary>
public sealed class FlagStateApiTests : IDisposable
{
    private const string States = "/api/v1/projects/shop/environments/prod/flag-states";

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

    private async Task Count(string env, string flag, string day)
    {
        var created = await server.Post($"/api/v1/projects/shop/environments/{env}/keys", new { kind = "server", name = env });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        var key = (await ApiServer.Json(created)).GetProperty("key").GetString();
        var sent = await server.Send(HttpMethod.Post, "/sdk/v1/events", $"Bearer {key}", new { events = new[] { new { flag, variationIndex = 0, count = 3, day } } });
        await ApiServer.Expect(HttpStatusCode.OK, sent);
    }

    private async Task<List<JsonElement>> Read(string path = States)
    {
        var response = await server.Get(path);
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return (await ApiServer.Json(response)).EnumerateArray().ToList();
    }

    [Fact]
    public async Task Each_flag_is_one_item_with_its_key_state_version_and_last_day_in_flag_id_order()
    {
        await Seed("zebra", "apple", "mango");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/apple/environments/prod/toggle", new { on = true }));
        await Count("prod", "mango", "2026-11-03");
        await Count("prod", "mango", "2026-11-04");

        var items = await Read();

        Assert.Equal(["zebra", "apple", "mango"], items.Select(i => i.GetProperty("flag").GetString()));
        Assert.All(items, i => Assert.Equal(["flag", "on", "version", "lastEvaluated"], i.EnumerateObject().Select(p => p.Name)));
        Assert.Equal([false, true, false], items.Select(i => i.GetProperty("on").GetBoolean()));
        Assert.Equal([1, 2, 1], items.Select(i => i.GetProperty("version").GetInt64()));
        Assert.Equal([null, null, "2026-11-04"], items.Select(i => i.GetProperty("lastEvaluated").GetString()));
    }

    [Fact]
    public async Task The_state_matches_what_the_flags_own_config_route_returns()
    {
        await Seed("dark-mode");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/dark-mode/environments/prod/toggle", new { on = true }));

        var config = await ApiServer.Json(await server.Get("/api/v1/projects/shop/flags/dark-mode/environments/prod"));
        var item = Assert.Single(await Read());

        Assert.Equal(config.GetProperty("on").GetBoolean(), item.GetProperty("on").GetBoolean());
        Assert.Equal(config.GetProperty("version").GetInt64(), item.GetProperty("version").GetInt64());
    }

    [Fact]
    public async Task Another_environment_has_its_own_state()
    {
        await Seed("dark-mode");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/dark-mode/environments/staging/toggle", new { on = true }));

        var prod = Assert.Single(await Read());
        var staging = Assert.Single(await Read("/api/v1/projects/shop/environments/staging/flag-states"));

        Assert.False(prod.GetProperty("on").GetBoolean());
        Assert.True(staging.GetProperty("on").GetBoolean());
    }

    [Fact]
    public async Task Archived_flags_are_left_out_and_a_project_with_no_flags_is_an_empty_array()
    {
        await Seed("kept", "gone");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/gone/archive"));

        Assert.Equal(["kept"], (await Read()).Select(i => i.GetProperty("flag").GetString()));

        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/kept/archive"));
        Assert.Empty(await Read());
    }

    [Fact]
    public async Task Other_projects_flags_are_not_listed()
    {
        await Seed("mine");
        await server.SeedProject("other");
        await server.SeedEnvironment("other", "prod");
        await server.SeedFlag("other", "theirs");

        Assert.Equal(["mine"], (await Read()).Select(i => i.GetProperty("flag").GetString()));
    }

    [Fact]
    public async Task Usage_in_another_environment_does_not_count_and_a_flag_never_evaluated_here_is_null()
    {
        await Seed("checkout", "banner");
        await Count("staging", "checkout", "2026-11-04");
        await Count("prod", "checkout", "2026-10-01");

        var items = await Read();
        var staging = await Read("/api/v1/projects/shop/environments/staging/flag-states");

        Assert.Equal(["2026-10-01", null], items.Select(i => i.GetProperty("lastEvaluated").GetString()));
        Assert.Equal(["2026-11-04", null], staging.Select(i => i.GetProperty("lastEvaluated").GetString()));
        var flag = await ApiServer.Json(await server.Get("/api/v1/projects/shop/flags/checkout"));
        Assert.Equal("2026-11-04", flag.GetProperty("lastEvaluated").GetString());
    }

    [Fact]
    public async Task An_unknown_project_or_environment_is_404()
    {
        await Seed("checkout");

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/nope/environments/prod/flag-states"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/shop/environments/nope/flag-states"));
    }

    [Fact]
    public async Task Only_the_admin_token_may_read_it_and_an_sdk_key_is_refused()
    {
        await Seed("checkout");
        var created = await ApiServer.Json(await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind = "server", name = "k" }));
        var key = created.GetProperty("key").GetString();

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await server.Send(HttpMethod.Get, States, null));
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await server.Send(HttpMethod.Get, States, "Bearer not-the-admin-token"));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Get, States, $"Bearer {key}"));
    }
}
