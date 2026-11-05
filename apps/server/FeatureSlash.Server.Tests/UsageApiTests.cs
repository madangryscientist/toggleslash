using System.Net;
using System.Net.Http.Json;
using System.Text;
using System.Text.Json;

/// <summary>Usage counts: <c>POST /sdk/v1/events</c>, what evaluate counts itself, and the two ways to read them back.</summary>
public sealed class UsageApiTests : IDisposable
{
    private const string Events = "/sdk/v1/events";
    private const string Evaluate = "/sdk/v1/evaluate";
    private const string Usage = "/api/v1/projects/shop/flags/checkout/usage";

    private static readonly string Today = DateTime.UtcNow.ToString("yyyy-MM-dd");
    private static readonly string LongAgo = DateTime.UtcNow.AddDays(-100).ToString("yyyy-MM-dd");

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("shop", "staging");
        await server.SeedFlag("shop", "checkout");
        await server.SeedFlag("shop", "banner", ["a", "b", "c"]);
    }

    private async Task<string> Key(string kind, string env = "prod")
    {
        var created = await server.Post($"/api/v1/projects/shop/environments/{env}/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private Task<HttpResponseMessage> Post(string? token, string path, object body) =>
        server.Send(HttpMethod.Post, path, token is null ? null : $"Bearer {token}", body);

    private static object Event(string flag, int variation, long count, string? day = null) =>
        new { flag, variationIndex = variation, count, day = day ?? Today };

    private Task<HttpResponseMessage> Send(string token, params object[] events) => Post(token, Events, new { events });

    /// <summary>Every stored row: environment key, flag key, variation, day, count.</summary>
    private List<(string Environment, string Flag, long Variation, string Day, long Count)> Rows()
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = """
            SELECT e.key, f.key, u.variation, u.day, u.count
            FROM flag_usage_daily u
            JOIN environments e ON e.id = u.environment_id
            JOIN flags f ON f.id = u.flag_id
            ORDER BY e.key, f.key, u.variation, u.day
            """;
        using var reader = command.ExecuteReader();
        var rows = new List<(string, string, long, string, long)>();
        while (reader.Read())
        {
            rows.Add((reader.GetString(0), reader.GetString(1), reader.GetInt64(2), reader.GetString(3), reader.GetInt64(4)));
        }
        return rows;
    }

    private static async Task ExpectCounted(int accepted, int unknown, HttpResponseMessage response)
    {
        await ApiServer.Expect(HttpStatusCode.OK, response);
        var body = await ApiServer.Json(response);
        Assert.Equal(["accepted", "unknownFlags"], body.EnumerateObject().Select(p => p.Name));
        Assert.Equal(accepted, body.GetProperty("accepted").GetInt32());
        Assert.Equal(unknown, body.GetProperty("unknownFlags").GetInt32());
    }

    // --- who may send ----------------------------------------------------------------------------------------

    [Fact]
    public async Task Either_kind_of_key_may_send_events_and_nothing_else_may()
    {
        await Seed();
        var body = new { events = new[] { Event("checkout", 0, 1) } };

        await ExpectCounted(1, 0, await Post(await Key("server"), Events, body));
        await ExpectCounted(1, 0, await Post(await Key("client"), Events, body));
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await Post(null, Events, body));
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await Post("not-a-key", Events, body));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await Post(ApiServer.AdminToken, Events, body));
        Assert.Equal(2, Rows().Single().Count);
    }

    [Fact]
    public async Task Reading_usage_is_for_the_admin_only()
    {
        await Seed();

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await server.Send(HttpMethod.Get, Usage, null));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Get, Usage, $"Bearer {await Key("server")}"));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get(Usage));
    }

    // --- accumulating ----------------------------------------------------------------------------------------

    [Fact]
    public async Task Batches_accumulate_into_one_row_per_flag_variation_and_day()
    {
        await Seed();
        var key = await Key("server");

        // Two entries for one row in a single batch are summed too.
        await ExpectCounted(3, 0, await Send(key, Event("checkout", 0, 5), Event("checkout", 0, 2), Event("checkout", 1, 1)));
        await ExpectCounted(2, 0, await Send(key, Event("checkout", 0, 10), Event("checkout", 0, 1, LongAgo)));

        Assert.Equal(
            [("prod", "checkout", 0, LongAgo, 1), ("prod", "checkout", 0, Today, 17), ("prod", "checkout", 1, Today, 1)],
            Rows());
    }

    [Fact]
    public async Task Concurrent_batches_lose_no_count()
    {
        await Seed();
        var key = await Key("server");

        var responses = await Task.WhenAll(Enumerable.Range(0, 20).Select(_ => Send(key, Event("checkout", 0, 3))));

        Assert.All(responses, r => Assert.Equal(HttpStatusCode.OK, r.StatusCode));
        Assert.Equal(60, Rows().Single().Count);
    }

    [Fact]
    public async Task An_empty_batch_is_fine_and_counts_nothing()
    {
        await Seed();

        await ExpectCounted(0, 0, await Send(await Key("server")));
        Assert.Empty(Rows());
    }

    // --- one environment, one project --------------------------------------------------------------------------

    [Fact]
    public async Task A_batch_counts_in_the_keys_own_environment_and_cannot_name_another()
    {
        await Seed();
        var staging = await Key("client", "staging");

        await ExpectCounted(1, 0, await Send(staging, Event("checkout", 0, 4)));
        // The environment is not a field: naming one is refused, not obeyed and not ignored.
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request",
            await Post(staging, Events, new { environment = "prod", events = new[] { Event("checkout", 0, 9) } }));
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request",
            await Post(staging, Events, new { events = new[] { new { flag = "checkout", variationIndex = 0, count = 9, day = Today, environment = "prod" } } }));

        Assert.Equal([("staging", "checkout", 0L, Today, 4L)], Rows());
    }

    [Fact]
    public async Task A_flag_of_another_project_is_an_unknown_flag()
    {
        await Seed();
        await server.SeedProject("other");
        await server.SeedEnvironment("other", "prod");
        await server.SeedFlag("other", "only-there");

        await ExpectCounted(0, 1, await Send(await Key("server"), Event("only-there", 0, 1)));

        Assert.Empty(Rows());
    }

    [Fact]
    public async Task Unknown_flags_are_ignored_and_counted_while_known_ones_still_count()
    {
        await Seed();

        await ExpectCounted(1, 2, await Send(await Key("server"), Event("nope", 0, 1), Event("checkout", 1, 6), Event("also-nope", 0, 1)));

        Assert.Equal([("prod", "checkout", 1L, Today, 6L)], Rows());
    }

    [Fact]
    public async Task An_archived_flag_still_counts()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/checkout/archive"));

        await ExpectCounted(1, 0, await Send(await Key("server"), Event("checkout", 0, 1)));
    }

    // --- limits and bad input ----------------------------------------------------------------------------------

    [Fact]
    public async Task A_batch_of_too_many_events_is_413_and_counts_nothing()
    {
        await Seed();
        var key = await Key("server");

        await ExpectCounted(1000, 0, await Send(key, Enumerable.Repeat(Event("checkout", 0, 1), 1000).ToArray()));
        await ApiServer.ExpectError(HttpStatusCode.RequestEntityTooLarge, "payload_too_large",
            await Send(key, Enumerable.Repeat(Event("checkout", 0, 1), 1001).ToArray()));

        Assert.Equal(1000, Rows().Single().Count);
    }

    [Fact]
    public async Task A_body_over_the_size_limit_is_413_however_few_events_it_holds()
    {
        await Seed();
        var key = await Key("server");

        await ApiServer.ExpectError(HttpStatusCode.RequestEntityTooLarge, "payload_too_large",
            await Send(key, Event(new string('x', 200 * 1024), 0, 1)));
        Assert.Empty(Rows());
    }

    [Fact]
    public async Task A_streamed_body_over_the_size_limit_is_413_without_a_content_length()
    {
        await Seed();
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Post, Events);
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {await Key("server")}");
        request.Content = new StreamContent(new MemoryStream(Encoding.UTF8.GetBytes("{\"events\":[" + new string(' ', 200 * 1024) + "]}")));
        request.Content.Headers.ContentType = new("application/json");
        request.Content.Headers.ContentLength = null;

        await ApiServer.ExpectError(HttpStatusCode.RequestEntityTooLarge, "payload_too_large", await client.SendAsync(request));
    }

    public static IEnumerable<object[]> BadEntries() =>
    [
        [new { variationIndex = 0, count = 1, day = Today }],
        [new { flag = "", variationIndex = 0, count = 1, day = Today }],
        [new { flag = "checkout", count = 1, day = Today }],
        [new { flag = "checkout", variationIndex = -1, count = 1, day = Today }],
        [new { flag = "checkout", variationIndex = 101, count = 1, day = Today }],
        [new { flag = "checkout", variationIndex = 0.5, count = 1, day = Today }],
        [new { flag = "checkout", variationIndex = 0, day = Today }],
        [new { flag = "checkout", variationIndex = 0, count = 0, day = Today }],
        [new { flag = "checkout", variationIndex = 0, count = -3, day = Today }],
        [new { flag = "checkout", variationIndex = 0, count = 1_000_000_001, day = Today }],
        [new { flag = "checkout", variationIndex = 0, count = 1 }],
        [new { flag = "checkout", variationIndex = 0, count = 1, day = "05/11/2026" }],
        [new { flag = "checkout", variationIndex = 0, count = 1, day = "2026-02-30" }],
        [new { flag = "checkout", variationIndex = 0, count = 1, day = DateTime.UtcNow.AddDays(3).ToString("yyyy-MM-dd") }],
        [new { flag = "checkout", variationIndex = 0, count = 1, day = DateTime.UtcNow.AddDays(-368).ToString("yyyy-MM-dd") }],
        [new { flag = "checkout", variationIndex = 0, count = 1, day = "0001-01-01" }],
        [new { flag = "checkout", variationIndex = "0", count = "1", day = Today }],
        // Anything that could identify an end user is refused, not stored and not quietly dropped.
        [new { flag = "checkout", variationIndex = 0, count = 1, day = Today, contextKey = "alice" }],
        [new { flag = "checkout", variationIndex = 0, count = 1, day = Today, context = new { key = "alice" } }],
    ];

    [Theory]
    [MemberData(nameof(BadEntries))]
    public async Task A_malformed_entry_is_a_400_and_the_whole_batch_counts_nothing(object bad)
    {
        await Seed();

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await Send(await Key("server"), Event("checkout", 0, 1), bad));

        Assert.Empty(Rows());
    }

    [Fact]
    public async Task A_variation_the_flag_does_not_have_is_a_400_and_the_whole_batch_counts_nothing()
    {
        await Seed();

        // checkout has two variations; banner has three.
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request",
            await Send(await Key("server"), Event("banner", 2, 1), Event("checkout", 2, 1)));

        Assert.Empty(Rows());
    }

    [Theory]
    [InlineData("not json")]
    [InlineData("[]")]
    [InlineData("{}")]
    [InlineData("{\"events\":null}")]
    [InlineData("{\"events\":[null]}")]
    [InlineData("{\"events\":\"x\"}")]
    [InlineData("")]
    public async Task A_body_that_is_not_a_batch_is_a_400(string body)
    {
        await Seed();
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Post, Events) { Content = new StringContent(body, Encoding.UTF8, "application/json") };
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {await Key("server")}");

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await client.SendAsync(request));
    }

    // --- evaluate counts itself --------------------------------------------------------------------------------

    [Fact]
    public async Task Evaluate_counts_the_variation_it_served_for_every_flag_it_evaluated()
    {
        await Seed();
        var key = await Key("client");
        await ApiServer.Expect(HttpStatusCode.OK, await Post(key, Evaluate, new { context = new { key = "alice-secret-id" } }));
        await ApiServer.Expect(HttpStatusCode.OK, await Post(key, Evaluate, new { context = new { key = "bob" }, flagKey = "checkout" }));

        // Both flags are off, so each serves its off variation (index 1).
        Assert.Equal(
            [("prod", "banner", 1L, Today, 1L), ("prod", "checkout", 1L, Today, 2L)],
            Rows());
    }

    [Fact]
    public async Task Evaluate_counts_nothing_for_a_request_it_refuses_or_an_evaluation_that_served_nothing()
    {
        await Seed();
        var key = await Key("client");

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await Post(key, Evaluate, new { context = new { key = "" } }));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await Post(key, Evaluate, new { context = new { key = "u" }, flagKey = "nope" }));
        await ApiServer.Expect(HttpStatusCode.OK, await Post(key, Evaluate, new { context = new { key = "u" }, flagKey = "checkout" }));
        // An archived flag is not in the ruleset, so it is not evaluated and not counted.
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/banner/archive"));
        await ApiServer.Expect(HttpStatusCode.OK, await Post(key, Evaluate, new { context = new { key = "u" } }));

        Assert.Equal([("prod", "checkout", 1L, Today, 2L)], Rows());
    }

    [Fact]
    public async Task Evaluate_counts_in_the_environment_of_its_key()
    {
        await Seed();

        await ApiServer.Expect(HttpStatusCode.OK, await Post(await Key("client", "staging"), Evaluate, new { context = new { key = "u" }, flagKey = "checkout" }));

        Assert.Equal([("staging", "checkout", 1L, Today, 1L)], Rows());
    }

    // --- reading ---------------------------------------------------------------------------------------------------

    [Fact]
    public async Task Usage_is_daily_totals_per_environment_with_the_variations_behind_them()
    {
        await Seed();
        var prod = await Key("server");
        var staging = await Key("server", "staging");
        await Send(prod, Event("checkout", 0, 5, "2026-11-03"), Event("checkout", 1, 2, "2026-11-03"), Event("checkout", 0, 1, "2026-11-04"));
        await Send(staging, Event("checkout", 1, 7, "2026-11-04"));

        var response = await server.Get($"{Usage}?since=2026-11-01&until=2026-11-05");

        await ApiServer.Expect(HttpStatusCode.OK, response);
        var body = await ApiServer.Json(response);
        Assert.Equal("checkout", body.GetProperty("flag").GetString());
        Assert.Equal("2026-11-01", body.GetProperty("since").GetString());
        Assert.Equal("2026-11-05", body.GetProperty("until").GetString());
        var environments = body.GetProperty("environments").EnumerateArray().ToList();
        Assert.Equal(["prod", "staging"], environments.Select(e => e.GetProperty("environment").GetString()));
        Assert.Equal(8, environments[0].GetProperty("total").GetInt64());
        Assert.Equal(7, environments[1].GetProperty("total").GetInt64());
        var days = environments[0].GetProperty("days").EnumerateArray().ToList();
        Assert.Equal(["2026-11-03", "2026-11-04"], days.Select(d => d.GetProperty("day").GetString()));
        Assert.Equal([7L, 1L], days.Select(d => d.GetProperty("count").GetInt64()));
        Assert.Equal(
            """[{"variationIndex":0,"count":5},{"variationIndex":1,"count":2}]""",
            days[0].GetProperty("variations").GetRawText());
    }

    [Fact]
    public async Task Usage_only_covers_the_window_and_lists_an_environment_with_none()
    {
        await Seed();
        await Send(await Key("server"), Event("checkout", 0, 5, "2026-10-01"), Event("checkout", 0, 1, Today));

        var recent = await ApiServer.Json(await server.Get(Usage));
        var old = await ApiServer.Json(await server.Get($"{Usage}?since=2026-09-30&until=2026-10-02"));

        var prod = recent.GetProperty("environments")[0];
        Assert.Equal(1, prod.GetProperty("total").GetInt64());
        Assert.Equal(Today, recent.GetProperty("until").GetString());
        Assert.Equal(30, (DateOnly.Parse(recent.GetProperty("until").GetString()!).DayNumber - DateOnly.Parse(recent.GetProperty("since").GetString()!).DayNumber) + 1);
        Assert.Equal(5, old.GetProperty("environments")[0].GetProperty("total").GetInt64());
        var staging = recent.GetProperty("environments")[1];
        Assert.Equal("staging", staging.GetProperty("environment").GetString());
        Assert.Equal(0, staging.GetProperty("total").GetInt64());
        Assert.Empty(staging.GetProperty("days").EnumerateArray());
    }

    [Theory]
    [InlineData("since=yesterday")]
    [InlineData("until=2026-13-01")]
    [InlineData("since=2026-11-05&until=2026-11-01")]
    [InlineData("since=2024-01-01&until=2026-01-01")]
    public async Task A_bad_window_is_a_400(string query)
    {
        await Seed();

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await server.Get($"{Usage}?{query}"));
    }

    [Fact]
    public async Task A_window_ending_at_the_start_of_time_is_empty_not_an_error()
    {
        await Seed();

        var response = await server.Get($"{Usage}?until=0001-01-01");

        await ApiServer.Expect(HttpStatusCode.OK, response);
        Assert.All((await ApiServer.Json(response)).GetProperty("environments").EnumerateArray(), e => Assert.Equal(0, e.GetProperty("total").GetInt64()));
    }

    [Fact]
    public async Task Usage_of_nothing_that_exists_is_404()
    {
        await Seed();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/nope/flags/checkout/usage"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/shop/flags/nope/usage"));
    }

    // --- lastEvaluated -----------------------------------------------------------------------------------------------

    [Fact]
    public async Task Last_evaluated_is_null_until_a_flag_is_counted_then_the_latest_day_in_any_environment()
    {
        await Seed();
        Assert.Equal(JsonValueKind.Null, (await ApiServer.Json(await server.Get("/api/v1/projects/shop/flags/checkout"))).GetProperty("lastEvaluated").ValueKind);

        await Send(await Key("server"), Event("checkout", 0, 1, "2026-11-02"), Event("banner", 0, 1, "2026-10-01"));
        await Send(await Key("server", "staging"), Event("checkout", 0, 1, "2026-11-04"));
        await Send(await Key("server"), Event("checkout", 0, 1, "2026-11-03"));

        Assert.Equal("2026-11-04", (await ApiServer.Json(await server.Get("/api/v1/projects/shop/flags/checkout"))).GetProperty("lastEvaluated").GetString());
        Assert.Equal("2026-10-01", (await ApiServer.Json(await server.Get("/api/v1/projects/shop/flags/banner"))).GetProperty("lastEvaluated").GetString());
    }

    [Fact]
    public async Task Last_evaluated_is_on_the_list_and_on_every_write_that_returns_a_flag()
    {
        await Seed();
        await server.SeedFlag("shop", "never");
        await Send(await Key("server"), Event("checkout", 0, 1, "2026-11-02"));

        var listed = (await ApiServer.Json(await server.Get("/api/v1/projects/shop/flags"))).EnumerateArray()
            .ToDictionary(f => f.GetProperty("key").GetString()!, f => f.GetProperty("lastEvaluated"));
        Assert.Equal("2026-11-02", listed["checkout"].GetString());
        Assert.Equal(JsonValueKind.Null, listed["banner"].ValueKind);
        Assert.Equal(JsonValueKind.Null, listed["never"].ValueKind);

        var created = await server.Post("/api/v1/projects/shop/flags", new { key = "fresh", name = "fresh", variations = new[] { true, false } });
        Assert.Equal(JsonValueKind.Null, (await ApiServer.Json(created)).GetProperty("lastEvaluated").ValueKind);
        foreach (var response in new[]
        {
            await server.Patch("/api/v1/projects/shop/flags/checkout", new { name = "Checkout" }),
            await server.Post("/api/v1/projects/shop/flags/checkout/archive"),
            await server.Post("/api/v1/projects/shop/flags/checkout/restore"),
        })
        {
            Assert.Equal("2026-11-02", (await ApiServer.Json(response)).GetProperty("lastEvaluated").GetString());
        }
    }

    [Fact]
    public async Task Evaluating_sets_last_evaluated_to_today()
    {
        await Seed();

        await Post(await Key("client"), Evaluate, new { context = new { key = "u" }, flagKey = "checkout" });

        Assert.Equal(Today, (await ApiServer.Json(await server.Get("/api/v1/projects/shop/flags/checkout"))).GetProperty("lastEvaluated").GetString());
    }

    [Fact]
    public async Task The_audit_trail_does_not_carry_last_evaluated()
    {
        await Seed();
        await Send(await Key("server"), Event("checkout", 0, 1));
        await server.Patch("/api/v1/projects/shop/flags/checkout", new { name = "Checkout" });

        var audited = server.AuditRows().Last(r => r.EntityType == "flag" && r.Action == "updated");

        Assert.DoesNotContain("lastEvaluated", audited.Before);
        Assert.DoesNotContain("lastEvaluated", audited.After);
    }

    // --- counts only ---------------------------------------------------------------------------------------------------

    [Fact]
    public async Task The_usage_table_holds_counts_only()
    {
        await Seed();
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT name FROM pragma_table_info('flag_usage_daily') ORDER BY cid";
        using var reader = command.ExecuteReader();
        var columns = new List<string>();
        while (reader.Read())
        {
            columns.Add(reader.GetString(0));
        }

        Assert.Equal(["environment_id", "flag_id", "variation", "day", "count"], columns);
    }

    [Fact]
    public async Task No_context_key_or_attribute_reaches_the_file()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.OK,
            await Post(await Key("client"), Evaluate, new { context = new { key = "alice-secret-id", attributes = new { email = "alice-secret@example.com" } } }));

        using var connection = server.OpenDatabase();
        using (var checkpoint = connection.CreateCommand())
        {
            checkpoint.CommandText = "PRAGMA wal_checkpoint(TRUNCATE)";
            checkpoint.ExecuteNonQuery();
        }
        var file = File.ReadAllText(connection.DataSource, Encoding.Latin1);

        Assert.Contains("checkout", file);
        Assert.DoesNotContain("alice", file);
        Assert.Equal(2, Rows().Count);
    }

    // --- CORS ------------------------------------------------------------------------------------------------------------

    [Fact]
    public async Task A_preflight_for_events_is_answered_without_a_token()
    {
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Options, Events);
        request.Headers.TryAddWithoutValidation("Origin", "https://app.example.com");
        request.Headers.TryAddWithoutValidation("Access-Control-Request-Method", "POST");
        request.Headers.TryAddWithoutValidation("Access-Control-Request-Headers", "authorization,content-type");

        var response = await client.SendAsync(request);

        Assert.Equal(HttpStatusCode.NoContent, response.StatusCode);
        Assert.Equal("*", response.Headers.GetValues("Access-Control-Allow-Origin").Single());
        Assert.False(response.Headers.Contains("Access-Control-Allow-Credentials"));
    }
}
