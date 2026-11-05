using System.Net;
using System.Text.Json;

/// <summary>Reading the trail: the project feed, a flag's history, their filters and the cursor.</summary>
public sealed class AuditApiTests : IDisposable
{
    private const string Audit = "/api/v1/projects/shop/audit";
    private const string History = "/api/v1/projects/shop/flags/checkout/history";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private static object ConfigBody(long version) => new
    {
        on = true,
        offVariation = 1,
        targets = Array.Empty<object>(),
        prerequisites = Array.Empty<object>(),
        rules = Array.Empty<object>(),
        fallthrough = new { variation = 0 },
        version,
    };

    /// <summary>Project shop with prod and staging, flags checkout and banner, and one save of checkout in prod.</summary>
    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("shop", "staging");
        await server.SeedFlag("shop", "checkout");
        await server.SeedFlag("shop", "banner");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put("/api/v1/projects/shop/flags/checkout/environments/prod", ConfigBody(1)));
    }

    private async Task<JsonElement> Page(string url)
    {
        var response = await server.Get(url);
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return await ApiServer.Json(response);
    }

    private static List<(string Type, string Action, string Id)> Summary(JsonElement page) =>
        page.GetProperty("events").EnumerateArray()
            .Select(e => (e.GetProperty("entityType").GetString()!, e.GetProperty("action").GetString()!, e.GetProperty("entityId").GetString()!))
            .ToList();

    [Fact]
    public async Task The_feed_lists_the_projects_events_newest_first_with_their_snapshots()
    {
        await Seed();

        var page = await Page(Audit);

        Assert.Equal(JsonValueKind.Null, page.GetProperty("nextCursor").ValueKind);
        // project, 2 environments, 2 flags, 1 config save: the newest first.
        Assert.Equal(
            [("flag_config", "updated", "checkout/prod"), ("flag", "created", "banner"), ("flag", "created", "checkout"), ("environment", "created", "staging"), ("environment", "created", "prod"), ("project", "created", "shop")],
            Summary(page));
        var newest = page.GetProperty("events")[0];
        Assert.Equal(("admin", "shop", "prod", "checkout"), (newest.GetProperty("actor").GetString(), newest.GetProperty("project").GetString(), newest.GetProperty("environment").GetString(), newest.GetProperty("flag").GetString()));
        Assert.Equal(1, newest.GetProperty("before").GetProperty("version").GetInt64());
        Assert.Equal(2, newest.GetProperty("after").GetProperty("version").GetInt64());
        Assert.Equal(JsonValueKind.Null, page.GetProperty("events")[5].GetProperty("before").ValueKind);
        Assert.True(DateTime.TryParse(newest.GetProperty("occurredAt").GetString(), out _));
    }

    [Fact]
    public async Task The_feed_holds_only_its_own_projects_events()
    {
        await Seed();
        await server.SeedProject("other");

        Assert.Equal(6, (await Page(Audit)).GetProperty("events").GetArrayLength());
        Assert.Single((await Page("/api/v1/projects/other/audit")).GetProperty("events").EnumerateArray());
    }

    [Fact]
    public async Task The_flag_filter_keeps_that_flags_events()
    {
        await Seed();

        Assert.Equal(
            [("flag_config", "updated", "checkout/prod"), ("flag", "created", "checkout")],
            Summary(await Page($"{Audit}?flag=checkout")));
        Assert.Empty((await Page($"{Audit}?flag=nope")).GetProperty("events").EnumerateArray());
    }

    [Fact]
    public async Task The_environment_filter_keeps_events_scoped_to_that_environment()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/checkout/environments/staging/toggle", new { on = true }));

        Assert.Equal(
            [("flag_config", "toggled", "checkout/staging"), ("environment", "created", "staging")],
            Summary(await Page($"{Audit}?environment=staging")));
        Assert.Equal(
            [("flag_config", "updated", "checkout/prod")],
            Summary(await Page($"{Audit}?environment=prod&flag=checkout")));
    }

    [Fact]
    public async Task Since_keeps_events_from_that_time_on()
    {
        await Seed();

        Assert.Equal(6, (await Page($"{Audit}?since=2000-01-01T00:00:00Z")).GetProperty("events").GetArrayLength());
        Assert.Equal(6, (await Page($"{Audit}?since=2000-01-01")).GetProperty("events").GetArrayLength());
        Assert.Empty((await Page($"{Audit}?since=2999-01-01T00:00:00Z")).GetProperty("events").EnumerateArray());
        Assert.Empty((await Page($"{Audit}?since={DateTime.UtcNow.AddMinutes(5):O}")).GetProperty("events").EnumerateArray());
    }

    [Fact]
    public async Task A_cursor_walks_the_feed_in_pages_without_a_gap_or_a_repeat()
    {
        await Seed();
        var seen = new List<long>();

        var url = $"{Audit}?limit=4";
        var pages = 0;
        while (true)
        {
            var page = await Page(url);
            pages++;
            seen.AddRange(page.GetProperty("events").EnumerateArray().Select(e => e.GetProperty("id").GetInt64()));
            if (page.GetProperty("nextCursor").GetString() is not { } cursor)
            {
                break;
            }
            Assert.Equal(4, page.GetProperty("events").GetArrayLength());
            url = $"{Audit}?limit=4&cursor={cursor}";
        }

        Assert.Equal(2, pages);
        Assert.Equal(server.AuditRows().Select(r => r.Id).OrderByDescending(id => id).ToList(), seen);
    }

    [Fact]
    public async Task A_page_that_ends_exactly_with_the_feed_has_no_cursor()
    {
        await Seed();

        var page = await Page($"{Audit}?limit=6");

        Assert.Equal(6, page.GetProperty("events").GetArrayLength());
        Assert.Equal(JsonValueKind.Null, page.GetProperty("nextCursor").ValueKind);
    }

    [Fact]
    public async Task A_new_event_does_not_move_a_cursor_already_handed_out()
    {
        await Seed();
        var first = await Page($"{Audit}?limit=3");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/checkout/archive"));

        var second = await Page($"{Audit}?limit=3&cursor={first.GetProperty("nextCursor").GetString()}");

        Assert.Equal(
            [("environment", "created", "staging"), ("environment", "created", "prod"), ("project", "created", "shop")],
            Summary(second));
    }

    [Theory]
    [InlineData("since=yesterday")]
    [InlineData("limit=0")]
    [InlineData("limit=201")]
    [InlineData("limit=-1")]
    [InlineData("cursor=abc")]
    [InlineData("cursor=-5")]
    public async Task A_bad_filter_is_400(string query)
    {
        await Seed();

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await server.Get($"{Audit}?{query}"));
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await server.Get($"{History}?{query}"));
    }

    [Fact]
    public async Task A_flags_history_has_its_metadata_archive_and_every_environments_config()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/checkout/environments/staging/toggle", new { on = true }));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Patch("/api/v1/projects/shop/flags/checkout", new { name = "Checkout v2" }));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/checkout/archive"));

        var page = await Page(History);

        Assert.Equal(
            [("flag", "archived", "checkout"), ("flag", "updated", "checkout"), ("flag_config", "toggled", "checkout/staging"), ("flag_config", "updated", "checkout/prod"), ("flag", "created", "checkout")],
            Summary(page));
        Assert.DoesNotContain("banner", page.GetRawText(), StringComparison.Ordinal);
    }

    [Fact]
    public async Task A_history_takes_the_same_filters()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/checkout/environments/staging/toggle", new { on = true }));

        Assert.Equal([("flag_config", "toggled", "checkout/staging")], Summary(await Page($"{History}?environment=staging")));
        var first = await Page($"{History}?limit=2");
        Assert.Equal(2, first.GetProperty("events").GetArrayLength());
        Assert.Equal(
            [("flag", "created", "checkout")],
            Summary(await Page($"{History}?limit=2&cursor={first.GetProperty("nextCursor").GetString()}")));
    }

    [Fact]
    public async Task An_unknown_project_or_flag_is_404()
    {
        await Seed();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/shop/flags/nope/history"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/nope/flags/checkout/history"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/nope/audit"));
    }

    [Fact]
    public async Task Reading_the_trail_needs_the_admin_token()
    {
        await Seed();
        var sdkKey = (await ApiServer.Json(await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind = "server", name = "backend" }))).GetProperty("key").GetString()!;

        foreach (var url in new[] { Audit, History })
        {
            await ApiServer.Expect(HttpStatusCode.Unauthorized, await server.Send(HttpMethod.Get, url, null));
            await ApiServer.Expect(HttpStatusCode.Unauthorized, await server.Send(HttpMethod.Get, url, "Bearer wrong"));
            await ApiServer.Expect(HttpStatusCode.Forbidden, await server.Send(HttpMethod.Get, url, $"Bearer {sdkKey}"));
        }
    }
}
