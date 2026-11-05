using System.Diagnostics;
using System.Net;
using System.Text.Json;
using System.Text.Json.Nodes;

/// <summary>
/// <c>GET /api/v1/projects/{project}/environments/{env}/rulesets/{hash}</c>: what the flags were under a hash this
/// environment served, from the bytes kept when <c>GET /sdk/v1/ruleset</c> first answered 200 with them.
/// </summary>
public sealed class RulesetLookupApiTests : IDisposable
{
    private const string Rulesets = "/api/v1/projects/shop/environments/prod/rulesets";
    private const string NeverServed = "0000000000000000000000000000000000000000000000000000000000000000";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("shop", "staging");
        await server.SeedFlag("shop", "checkout");
    }

    private async Task<string> Key(string kind, string env = "prod")
    {
        var created = await server.Post($"/api/v1/projects/shop/environments/{env}/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private async Task<string> Mint(string project, string permission)
    {
        var created = await server.Post($"/api/v1/projects/{project}/tokens", new { name = permission, permission, expiresInDays = 30 });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("token").GetString()!;
    }

    /// <summary>Reads the ruleset as an SDK does and returns its hash: the ETag without quotes.</summary>
    private async Task<string> Serve(string env = "prod")
    {
        var response = await server.Send(HttpMethod.Get, "/sdk/v1/ruleset", $"Bearer {await Key("server", env)}");
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return response.Headers.ETag!.Tag.Trim('"');
    }

    private Task Toggle(string flag, bool on) =>
        server.Post($"/api/v1/projects/shop/flags/{flag}/environments/prod/toggle", new { on });

    private async Task Save(string flag, string configJson)
    {
        var path = $"/api/v1/projects/shop/flags/{flag}/environments/prod";
        var version = (await ApiServer.Json(await server.Get(path))).GetProperty("version").GetInt64();
        var config = JsonNode.Parse(configJson)!.AsObject();
        config["version"] = version;
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put(path, JsonDocument.Parse(config.ToJsonString()).RootElement));
    }

    private async Task Segment(string key, string[]? included = null) =>
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects/shop/environments/prod/segments", new
        {
            key,
            included = included ?? [],
            excluded = Array.Empty<string>(),
            rules = Array.Empty<object>(),
        }));

    private async Task<JsonElement> Lookup(string hash, string query = "")
    {
        var response = await server.Get($"{Rulesets}/{hash}{query}");
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return await ApiServer.Json(response);
    }

    private long RowCount()
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT count(*) FROM ruleset_snapshots";
        return (long)command.ExecuteScalar()!;
    }

    private static string[] Keys(JsonElement body, string list) =>
        [.. body.GetProperty(list).EnumerateArray().Select(e => e.GetProperty("key").GetString()!)];

    [Fact]
    public async Task A_write_changes_the_hash_and_the_old_hash_still_answers_the_old_values()
    {
        await Seed();
        await Toggle("checkout", false);
        var before = DateTime.UtcNow.AddSeconds(-5);
        var oldHash = await Serve();

        await Toggle("checkout", true);
        var newHash = await Serve();
        Assert.NotEqual(oldHash, newHash);

        var old = await Lookup(oldHash);
        Assert.Equal($"\"{oldHash}\"", old.GetProperty("etag").GetString());
        Assert.False(old.GetProperty("flags")[0].GetProperty("on").GetBoolean());
        var firstServedAt = old.GetProperty("firstServedAt");
        Assert.EndsWith("Z", firstServedAt.GetString());
        Assert.InRange(firstServedAt.GetDateTime().ToUniversalTime(), before, DateTime.UtcNow.AddSeconds(5));

        Assert.True((await Lookup(newHash)).GetProperty("flags")[0].GetProperty("on").GetBoolean());
    }

    [Fact]
    public async Task The_answer_has_the_fields_the_brief_names_and_the_values_that_were_saved()
    {
        await Seed();
        await Segment("beta", ["u1"]);
        await Save("checkout", """
            { "on": true, "offVariation": 1, "targets": [{ "variation": 0, "contextKeys": ["vip"] }], "prerequisites": [],
              "rules": [{ "clauses": [{ "op": "segmentMatch", "values": ["beta"] }], "variation": 0 }],
              "fallthrough": { "variation": 1 } }
            """);

        var body = await Lookup(await Serve());

        Assert.Equal(["etag", "firstServedAt", "flags", "segments"], body.EnumerateObject().Select(p => p.Name).Order().ToArray());
        var flag = body.GetProperty("flags")[0];
        Assert.Equal(
            ["fallthrough", "key", "offVariation", "on", "prerequisites", "rules", "targets", "variations", "version"],
            flag.EnumerateObject().Select(p => p.Name).Order().ToArray());
        Assert.Equal("checkout", flag.GetProperty("key").GetString());
        Assert.Equal("[true,false]", flag.GetProperty("variations").GetRawText());
        Assert.Equal("""{"variation":1}""", flag.GetProperty("fallthrough").GetRawText());
        Assert.Equal("vip", flag.GetProperty("targets")[0].GetProperty("contextKeys")[0].GetString());
        var segment = body.GetProperty("segments")[0];
        Assert.Equal(["excluded", "included", "key", "rules"], segment.EnumerateObject().Select(p => p.Name).Order().ToArray());
        Assert.Equal("u1", segment.GetProperty("included")[0].GetString());
    }

    [Fact]
    public async Task A_hash_never_served_is_404_with_a_message_saying_so()
    {
        await Seed();

        var response = await server.Get($"{Rulesets}/{NeverServed}");

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", response);
        Assert.Contains("never served", (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
    }

    [Fact]
    public async Task The_hash_a_server_would_serve_but_has_not_is_404_because_a_flag_write_stores_nothing()
    {
        await Seed();
        await Toggle("checkout", true);
        var runtime = await ApiServer.Json(await server.Get("/api/v1/projects/shop/environments/prod/runtime"));

        Assert.Equal(0, RowCount());
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Rulesets}/{runtime.GetProperty("etag").GetString()!.Trim('"')}"));
        Assert.Equal(0, RowCount());
    }

    [Fact]
    public async Task A_quoted_or_capitalised_hash_is_not_the_hash()
    {
        await Seed();
        var hash = await Serve();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Rulesets}/{Uri.EscapeDataString($"\"{hash}\"")}"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Rulesets}/{hash.ToUpperInvariant()}"));
    }

    [Fact]
    public async Task The_wrong_environment_or_project_is_404()
    {
        await Seed();
        await server.SeedProject("other");
        await server.SeedEnvironment("other", "prod");
        var hash = await Serve();

        foreach (var path in new[]
        {
            $"/api/v1/projects/shop/environments/staging/rulesets/{hash}",
            $"/api/v1/projects/other/environments/prod/rulesets/{hash}",
            $"/api/v1/projects/nope/environments/prod/rulesets/{hash}",
            $"/api/v1/projects/shop/environments/nope/rulesets/{hash}",
        })
        {
            await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get(path));
        }
    }

    [Fact]
    public async Task No_salt_is_anywhere_in_the_answer_narrowed_or_not()
    {
        await Seed();
        await Segment("beta");
        await Save("checkout", """
            { "on": true, "offVariation": 1, "targets": [], "prerequisites": [],
              "rules": [{ "clauses": [{ "op": "segmentMatch", "values": ["beta"] }], "variation": 0 }],
              "fallthrough": { "rollout": { "variations": [{ "variation": 0, "weight": 100000 }] } } }
            """);
        var hash = await Serve();
        var salts = new[] { server.Salt("flags", "shop", "checkout"), server.Salt("segments", "shop", "beta") };

        // The kept bytes do hold them, or this test would prove nothing.
        using (var connection = server.OpenDatabase())
        using (var command = connection.CreateCommand())
        {
            command.CommandText = "SELECT CAST(body AS TEXT) FROM ruleset_snapshots";
            var kept = (string)command.ExecuteScalar()!;
            Assert.All(salts, salt => Assert.Contains(salt, kept));
        }

        foreach (var query in new[] { "", "?flag=checkout" })
        {
            var text = await (await server.Get($"{Rulesets}/{hash}{query}")).Content.ReadAsStringAsync();
            Assert.DoesNotContain("salt", text, StringComparison.OrdinalIgnoreCase);
            Assert.All(salts, salt => Assert.DoesNotContain(salt, text));
        }
    }

    [Fact]
    public async Task A_flag_narrows_to_it_and_the_segments_its_rules_name()
    {
        await Seed();
        await server.SeedFlag("shop", "search");
        await Segment("beta");
        await Segment("gamma");
        await Segment("unused");
        await Save("checkout", """
            { "on": true, "offVariation": 1, "targets": [], "prerequisites": [],
              "rules": [
                { "clauses": [{ "op": "segmentMatch", "values": ["beta", "gamma"] }], "variation": 0 },
                { "clauses": [{ "attribute": "beta", "op": "in", "values": ["unused"] }], "variation": 0 }],
              "fallthrough": { "variation": 0 } }
            """);
        var hash = await Serve();

        var narrowed = await Lookup(hash, "?flag=checkout");
        Assert.Equal(["checkout"], Keys(narrowed, "flags"));
        Assert.Equal(["beta", "gamma"], Keys(narrowed, "segments"));

        var noSegments = await Lookup(hash, "?flag=search");
        Assert.Equal(["search"], Keys(noSegments, "flags"));
        Assert.Empty(Keys(noSegments, "segments"));

        var whole = await Lookup(hash);
        Assert.Equal(["checkout", "search"], Keys(whole, "flags"));
        Assert.Equal(["beta", "gamma", "unused"], Keys(whole, "segments"));
    }

    [Fact]
    public async Task An_unknown_flag_in_a_known_ruleset_is_404()
    {
        await Seed();
        var hash = await Serve();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Rulesets}/{hash}?flag=absent"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Rulesets}/{hash}?flag="));
    }

    [Fact]
    public async Task A_read_token_of_the_project_passes_and_nothing_else_does()
    {
        await Seed();
        await server.SeedProject("other");
        var hash = await Serve();
        var path = $"{Rulesets}/{hash}";

        await ApiServer.Expect(HttpStatusCode.OK, await server.Send(HttpMethod.Get, path, $"Bearer {await Mint("shop", "read")}"));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Send(HttpMethod.Get, path, $"Bearer {await Mint("shop", "write")}"));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Get, path, $"Bearer {await Mint("other", "read")}"));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Get, path, $"Bearer {await Key("server")}"));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Get, path, $"Bearer {await Key("client")}"));
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await server.Send(HttpMethod.Get, path, null));
    }

    [Fact]
    public async Task A_second_200_of_the_same_body_adds_no_row_and_keeps_the_first_time()
    {
        await Seed();
        var hash = await Serve();
        var first = (await Lookup(hash)).GetProperty("firstServedAt").GetString();
        Assert.Equal(1, RowCount());

        Assert.Equal(hash, await Serve());

        Assert.Equal(1, RowCount());
        Assert.Equal(first, (await Lookup(hash)).GetProperty("firstServedAt").GetString());
    }

    [Fact]
    public async Task Many_first_reads_at_once_all_succeed_and_are_one_row()
    {
        await Seed();
        var key = await Key("server");

        var responses = await Task.WhenAll(Enumerable.Range(0, 20).Select(_ => server.Send(HttpMethod.Get, "/sdk/v1/ruleset", $"Bearer {key}")));

        Assert.All(responses, r => Assert.Equal(HttpStatusCode.OK, r.StatusCode));
        Assert.Equal(1, RowCount());
    }

    [Fact]
    public async Task A_304_writes_nothing()
    {
        await Seed();
        // Held from before anything was kept: the tag matches, and the answer stores nothing.
        var runtime = await ApiServer.Json(await server.Get("/api/v1/projects/shop/environments/prod/runtime"));
        var etag = runtime.GetProperty("etag").GetString()!;
        var key = await Key("server");

        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Get, "/sdk/v1/ruleset");
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {key}");
        request.Headers.TryAddWithoutValidation("If-None-Match", etag);
        var response = await client.SendAsync(request);

        Assert.Equal(HttpStatusCode.NotModified, response.StatusCode);
        Assert.Equal(0, RowCount());
    }

    [Fact]
    public async Task A_kept_ruleset_is_served_again_without_the_write_lock()
    {
        await Seed();
        await Serve();
        var key = await Key("server");

        using var connection = server.OpenDatabase();
        using var writer = connection.BeginTransaction(System.Data.IsolationLevel.Serializable, deferred: false);
        var clock = Stopwatch.StartNew();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Send(HttpMethod.Get, "/sdk/v1/ruleset", $"Bearer {key}"));

        // SQLite waits 30 seconds for a lock it cannot get; a request that wanted one would still be waiting.
        Assert.True(clock.Elapsed < TimeSpan.FromSeconds(15), $"took {clock.Elapsed}");
    }

    [Fact]
    public async Task A_failure_to_keep_a_ruleset_still_serves_it()
    {
        await Seed();
        using (var connection = server.OpenDatabase())
        using (var command = connection.CreateCommand())
        {
            command.CommandText = "CREATE TRIGGER refuse BEFORE INSERT ON ruleset_snapshots BEGIN SELECT RAISE(ABORT, 'refused'); END";
            command.ExecuteNonQuery();
        }

        var hash = await Serve();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Rulesets}/{hash}"));
    }
}
