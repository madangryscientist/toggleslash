using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Nodes;

/// <summary>The SDK read API: the ruleset with its ETag, evaluate, and the CORS that only evaluate has.</summary>
public sealed class SdkApiTests : IDisposable
{
    private const string Ruleset = "/sdk/v1/ruleset";
    private const string Evaluate = "/sdk/v1/evaluate";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private static string Config(string flag, string env = "prod") => $"/api/v1/projects/shop/flags/{flag}/environments/{env}";

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

    private async Task<string> Key(string kind, string env = "prod")
    {
        var created = await server.Post($"/api/v1/projects/shop/environments/{env}/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private async Task<HttpResponseMessage> Call(HttpMethod method, string path, string? token, object? body = null, params (string Name, string Value)[] headers)
    {
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(method, path);
        if (token is not null)
        {
            request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {token}");
        }
        foreach (var (name, value) in headers)
        {
            request.Headers.TryAddWithoutValidation(name, value);
        }
        if (body is not null)
        {
            request.Content = JsonContent.Create(body);
        }
        return await client.SendAsync(request);
    }

    private Task<HttpResponseMessage> GetRuleset(string token, string? ifNoneMatch = null) =>
        Call(HttpMethod.Get, Ruleset, token, null, ifNoneMatch is null ? [] : [("If-None-Match", ifNoneMatch)]);

    private Task<HttpResponseMessage> Post(string? token, object body, params (string Name, string Value)[] headers) =>
        Call(HttpMethod.Post, Evaluate, token, body, headers);

    private async Task<string> ETagOf(string token)
    {
        var response = await GetRuleset(token);
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return response.Headers.ETag!.ToString();
    }

    private async Task Save(string flag, string configJson, string env = "prod")
    {
        var version = (await ApiServer.Json(await server.Get(Config(flag, env)))).GetProperty("version").GetInt64();
        var config = JsonNode.Parse(configJson)!.AsObject();
        config["version"] = version;
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put(Config(flag, env), JsonDocument.Parse(config.ToJsonString()).RootElement));
    }

    private const string Targeted = """
        { "on": true, "offVariation": 1,
          "targets": [{ "variation": 1, "contextKeys": ["vip-secret-user"] }],
          "prerequisites": [],
          "rules": [{ "clauses": [{ "attribute": "internal-attribute", "op": "in", "values": ["hidden-value"] }], "variation": 1 }],
          "fallthrough": { "variation": 0 } }
        """;

    // --- the ruleset ---------------------------------------------------------------------------------------

    [Fact]
    public async Task The_ruleset_needs_a_server_key()
    {
        await Seed("dark-mode");
        var serverKey = await Key("server");
        var clientKey = await Key("client");

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await GetRuleset("not-a-key"));
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await Call(HttpMethod.Get, Ruleset, null));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await GetRuleset(clientKey));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await GetRuleset(ApiServer.AdminToken));
        await ApiServer.Expect(HttpStatusCode.OK, await GetRuleset(serverKey));
    }

    [Fact]
    public async Task A_revoked_server_key_is_refused()
    {
        await Seed("dark-mode");
        var created = await ApiServer.Json(await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind = "server", name = "old" }));
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"/api/v1/projects/shop/environments/prod/keys/{created.GetProperty("id").GetInt64()}"));

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await GetRuleset(created.GetProperty("key").GetString()!));
    }

    [Fact]
    public async Task The_ruleset_is_the_environments_flags_and_segments_in_the_shape_of_the_spec()
    {
        await Seed("dark-mode", "beta-ui");
        await Save("dark-mode", Targeted);
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects/shop/environments/prod/segments", new { key = "testers", included = new[] { "u1" }, excluded = Array.Empty<string>(), rules = Array.Empty<object>() }));
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects/shop/environments/staging/segments", new { key = "staging-only", included = Array.Empty<string>(), excluded = Array.Empty<string>(), rules = Array.Empty<object>() }));

        var response = await GetRuleset(await Key("server"));

        await ApiServer.Expect(HttpStatusCode.OK, response);
        Assert.Equal("application/json", response.Content.Headers.ContentType!.MediaType);
        var body = await ApiServer.Json(response);
        Assert.Equal(["flags", "segments"], body.EnumerateObject().Select(p => p.Name));
        var flags = body.GetProperty("flags").EnumerateArray().ToList();
        Assert.Equal(["beta-ui", "dark-mode"], flags.Select(f => f.GetProperty("key").GetString()));
        var dark = flags[1];
        Assert.Equal(
            ["key", "variations", "on", "offVariation", "targets", "prerequisites", "rules", "fallthrough", "salt", "version"],
            dark.EnumerateObject().Select(p => p.Name));
        Assert.True(dark.GetProperty("on").GetBoolean());
        Assert.Equal(2, dark.GetProperty("version").GetInt64());
        Assert.Equal("[true,false]", dark.GetProperty("variations").GetRawText());
        Assert.Equal(server.Salt("flags", "shop", "dark-mode"), dark.GetProperty("salt").GetString());
        Assert.Equal("vip-secret-user", dark.GetProperty("targets")[0].GetProperty("contextKeys")[0].GetString());
        // Only this environment's segments.
        var segment = Assert.Single(body.GetProperty("segments").EnumerateArray());
        Assert.Equal("testers", segment.GetProperty("key").GetString());
        Assert.Equal(server.Salt("segments", "shop", "testers"), segment.GetProperty("salt").GetString());
        Assert.Equal(["key", "salt", "included", "excluded", "rules"], segment.EnumerateObject().Select(p => p.Name));
    }

    [Fact]
    public async Task Archived_flags_are_left_out_and_come_back_when_restored()
    {
        await Seed("dark-mode", "old-flag");
        var token = await Key("server");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/old-flag/archive"));

        var archived = await ApiServer.Json(await GetRuleset(token));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/old-flag/restore"));
        var restored = await ApiServer.Json(await GetRuleset(token));

        Assert.Equal(["dark-mode"], archived.GetProperty("flags").EnumerateArray().Select(f => f.GetProperty("key").GetString()));
        Assert.Equal(["dark-mode", "old-flag"], restored.GetProperty("flags").EnumerateArray().Select(f => f.GetProperty("key").GetString()));
    }

    [Fact]
    public async Task The_ruleset_of_a_project_with_nothing_in_it_is_empty()
    {
        await Seed();

        var body = await ApiServer.Json(await GetRuleset(await Key("server")));

        Assert.Equal("""{"flags":[],"segments":[]}""", body.GetRawText());
    }

    [Fact]
    public async Task Each_key_reads_its_own_environment()
    {
        await Seed("dark-mode");
        await Save("dark-mode", Targeted);
        var prod = await ApiServer.Json(await GetRuleset(await Key("server", "prod")));
        var staging = await ApiServer.Json(await GetRuleset(await Key("server", "staging")));

        Assert.True(prod.GetProperty("flags")[0].GetProperty("on").GetBoolean());
        Assert.False(staging.GetProperty("flags")[0].GetProperty("on").GetBoolean());
    }

    // --- the ETag ------------------------------------------------------------------------------------------

    [Fact]
    public async Task The_ETag_is_strong_and_stable_while_nothing_is_written()
    {
        await Seed("dark-mode");
        var token = await Key("server");

        var first = await ETagOf(token);
        var second = await ETagOf(token);

        Assert.Equal(first, second);
        Assert.StartsWith("\"", first);
        Assert.False(first.StartsWith("W/", StringComparison.Ordinal));
        Assert.Matches("^\"[0-9a-f]{64}\"$", first);
    }

    [Fact]
    public async Task The_ETag_is_stable_across_reads_by_other_means_and_other_keys()
    {
        await Seed("dark-mode");
        var token = await Key("server");
        var before = await ETagOf(token);

        await server.Get(Config("dark-mode"));
        await ApiServer.Expect(HttpStatusCode.OK, await Post(await Key("client"), new { context = new { key = "u" } }));
        var other = await ETagOf(await Key("server"));

        Assert.Equal(before, await ETagOf(token));
        Assert.Equal(before, other);
    }

    [Fact]
    public async Task The_ETag_changes_on_every_kind_of_config_write()
    {
        await Seed("dark-mode");
        var token = await Key("server");
        var seen = new List<string> { await ETagOf(token) };

        await Save("dark-mode", Targeted);
        seen.Add(await ETagOf(token));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post(Config("dark-mode") + "/toggle", new { on = false }));
        seen.Add(await ETagOf(token));
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects/shop/environments/prod/segments", new { key = "testers", included = new[] { "u1" }, excluded = Array.Empty<string>(), rules = Array.Empty<object>() }));
        seen.Add(await ETagOf(token));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put("/api/v1/projects/shop/environments/prod/segments/testers", new { included = new[] { "u1", "u2" }, excluded = Array.Empty<string>(), rules = Array.Empty<object>() }));
        seen.Add(await ETagOf(token));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/dark-mode/archive"));
        seen.Add(await ETagOf(token));

        Assert.Equal(seen.Count, seen.Distinct().Count());
    }

    [Fact]
    public async Task A_write_to_another_environment_leaves_the_ETag_alone()
    {
        await Seed("dark-mode");
        var token = await Key("server");
        var before = await ETagOf(token);

        await Save("dark-mode", Targeted, env: "staging");

        Assert.Equal(before, await ETagOf(token));
    }

    [Theory]
    [InlineData("exact")]
    [InlineData("weak")]
    [InlineData("in a list")]
    [InlineData("any")]
    public async Task A_matching_If_None_Match_is_a_304_with_no_body(string form)
    {
        await Seed("dark-mode");
        var token = await Key("server");
        var tag = await ETagOf(token);
        var header = form switch
        {
            "exact" => tag,
            "weak" => "W/" + tag,
            "in a list" => $"\"nope\", {tag}",
            _ => "*",
        };

        var response = await GetRuleset(token, header);

        Assert.Equal(HttpStatusCode.NotModified, response.StatusCode);
        Assert.Equal(tag, response.Headers.ETag!.ToString());
        Assert.Equal("", await response.Content.ReadAsStringAsync());
    }

    [Fact]
    public async Task A_stale_or_garbled_If_None_Match_gets_the_ruleset()
    {
        await Seed("dark-mode");
        var token = await Key("server");
        var stale = await ETagOf(token);
        await Save("dark-mode", Targeted);

        await ApiServer.Expect(HttpStatusCode.OK, await GetRuleset(token, stale));
        await ApiServer.Expect(HttpStatusCode.OK, await GetRuleset(token, "not an etag"));
        Assert.Equal(HttpStatusCode.NotModified, (await GetRuleset(token, await ETagOf(token))).StatusCode);
    }

    [Fact]
    public async Task The_ruleset_must_be_revalidated_and_a_304_still_needs_a_key()
    {
        await Seed("dark-mode");
        var token = await Key("server");
        var response = await GetRuleset(token);
        var tag = response.Headers.ETag!.ToString();

        Assert.True(response.Headers.CacheControl!.NoCache);
        Assert.True(response.Headers.CacheControl.Private);
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await GetRuleset("not-a-key", tag));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await GetRuleset(await Key("client"), tag));
    }

    // --- evaluate ------------------------------------------------------------------------------------------

    [Fact]
    public async Task Evaluate_takes_a_server_or_a_client_key_and_nothing_else()
    {
        await Seed("dark-mode");
        var body = new { context = new { key = "u1" } };

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await Post(null, body));
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await Post("not-a-key", body));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await Post(ApiServer.AdminToken, body));
        await ApiServer.Expect(HttpStatusCode.OK, await Post(await Key("server"), body));
        await ApiServer.Expect(HttpStatusCode.OK, await Post(await Key("client"), body));
    }

    [Fact]
    public async Task Evaluate_answers_for_every_flag_that_is_not_archived()
    {
        await Seed("dark-mode", "beta-ui", "old-flag");
        await Save("dark-mode", Targeted);
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/old-flag/archive"));

        var response = await Post(await Key("client"), new { context = new { key = "u1" } });

        await ApiServer.Expect(HttpStatusCode.OK, response);
        var flags = (await ApiServer.Json(response)).GetProperty("flags");
        Assert.Equal(["beta-ui", "dark-mode"], flags.EnumerateObject().Select(p => p.Name));
        Assert.Equal("""{"value":false,"variationIndex":1,"reason":{"kind":"OFF"}}""", flags.GetProperty("beta-ui").GetRawText());
        Assert.Equal("""{"value":true,"variationIndex":0,"reason":{"kind":"FALLTHROUGH"}}""", flags.GetProperty("dark-mode").GetRawText());
    }

    [Fact]
    public async Task Evaluate_can_ask_for_one_flag()
    {
        await Seed("dark-mode", "beta-ui");
        await Save("dark-mode", Targeted);
        var client = await Key("client");

        var matching = await ApiServer.Json(await Post(client, new
        {
            flagKey = "dark-mode",
            context = new { key = "u1", attributes = new Dictionary<string, object> { ["internal-attribute"] = "hidden-value" } },
        }));
        var target = await ApiServer.Json(await Post(client, new { flagKey = "dark-mode", context = new { key = "vip-secret-user" } }));

        var only = Assert.Single(matching.GetProperty("flags").EnumerateObject());
        Assert.Equal("dark-mode", only.Name);
        Assert.Equal("""{"value":false,"variationIndex":1,"reason":{"kind":"RULE_MATCH","ruleIndex":0}}""", only.Value.GetRawText());
        Assert.Equal(
            """{"value":false,"variationIndex":1,"reason":{"kind":"TARGET_MATCH"}}""",
            target.GetProperty("flags").GetProperty("dark-mode").GetRawText());
    }

    [Fact]
    public async Task Evaluate_follows_a_write_straight_away()
    {
        await Seed("dark-mode");
        var client = await Key("client");
        var ask = () => Post(client, new { flagKey = "dark-mode", context = new { key = "u1" } });

        var off = (await ApiServer.Json(await ask())).GetProperty("flags").GetProperty("dark-mode");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post(Config("dark-mode") + "/toggle", new { on = true }));
        var on = (await ApiServer.Json(await ask())).GetProperty("flags").GetProperty("dark-mode");

        Assert.Equal("OFF", off.GetProperty("reason").GetProperty("kind").GetString());
        Assert.Equal("FALLTHROUGH", on.GetProperty("reason").GetProperty("kind").GetString());
    }

    [Fact]
    public async Task Evaluate_serves_the_environment_of_the_key()
    {
        await Seed("dark-mode");
        await Save("dark-mode", Targeted);

        var prod = await ApiServer.Json(await Post(await Key("client", "prod"), new { context = new { key = "u1" } }));
        var staging = await ApiServer.Json(await Post(await Key("client", "staging"), new { context = new { key = "u1" } }));

        Assert.Equal("FALLTHROUGH", prod.GetProperty("flags").GetProperty("dark-mode").GetProperty("reason").GetProperty("kind").GetString());
        Assert.Equal("OFF", staging.GetProperty("flags").GetProperty("dark-mode").GetProperty("reason").GetProperty("kind").GetString());
    }

    [Fact]
    public async Task A_flag_that_does_not_exist_or_is_archived_is_a_404()
    {
        await Seed("dark-mode", "old-flag");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/old-flag/archive"));
        var client = await Key("client");

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await Post(client, new { flagKey = "nope", context = new { key = "u1" } }));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await Post(client, new { flagKey = "old-flag", context = new { key = "u1" } }));
    }

    [Fact]
    public async Task An_archived_prerequisite_makes_its_dependant_an_error_not_a_crash()
    {
        await Seed("parent", "child");
        await Save("child", """
            { "on": true, "offVariation": 1, "targets": [], "prerequisites": [{ "flagKey": "parent", "variation": 0 }], "rules": [], "fallthrough": { "variation": 0 } }
            """);
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/parent/archive"));

        var body = await ApiServer.Json(await Post(await Key("client"), new { context = new { key = "u1" } }));

        Assert.Equal(
            """{"value":null,"variationIndex":null,"reason":{"kind":"ERROR","code":"FLAG_NOT_FOUND"}}""",
            body.GetProperty("flags").GetProperty("child").GetRawText());
    }

    [Theory]
    [InlineData("""{}""")]
    [InlineData("""{"context":null}""")]
    [InlineData("""{"context":"u1"}""")]
    [InlineData("""{"context":[]}""")]
    [InlineData("""{"context":{}}""")]
    [InlineData("""{"context":{"key":""}}""")]
    [InlineData("""{"context":{"key":7}}""")]
    [InlineData("""{"context":{"key":"u1","attributes":"x"}}""")]
    [InlineData("""{"context":{"key":"u1"},"extra":1}""")]
    [InlineData("""{"context":{"key":"u1"},"flagKey":7}""")]
    public async Task A_bad_request_is_a_400_even_when_there_is_nothing_to_evaluate(string json)
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Post, Evaluate)
        {
            Content = new StringContent(json, new MediaTypeHeaderValue("application/json")),
        };
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {await Key("client")}");

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await client.SendAsync(request));
    }

    [Fact]
    public async Task Evaluate_exposes_no_rules_targets_segments_salts_or_other_contexts()
    {
        await Seed("dark-mode", "beta-ui");
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects/shop/environments/prod/segments", new
        {
            key = "secret-segment-name",
            included = new[] { "included-secret-user" },
            excluded = new[] { "excluded-secret-user" },
            rules = new[] { new { clauses = new[] { new { attribute = "segment-attribute", op = "in", values = new[] { "segment-hidden-value" } } }, weight = 100000 } },
        }));
        await Save("dark-mode", Targeted);
        await Save("beta-ui", """
            { "on": true, "offVariation": 1, "targets": [],
              "prerequisites": [{ "flagKey": "dark-mode", "variation": 1 }],
              "rules": [{ "clauses": [{ "op": "segmentMatch", "values": ["secret-segment-name"] }], "variation": 1 }],
              "fallthrough": { "variation": 0 } }
            """);
        var secrets = new[]
        {
            "vip-secret-user", "internal-attribute", "hidden-value", "secret-segment-name", "included-secret-user",
            "excluded-secret-user", "segment-attribute", "segment-hidden-value", server.Salt("flags", "shop", "dark-mode"),
            server.Salt("flags", "shop", "beta-ui"), server.Salt("segments", "shop", "secret-segment-name"),
            "targets", "rules", "clauses", "contextKeys", "segments", "salt", "included", "excluded", "fallthrough", "prerequisites",
        };
        var client = await Key("client");
        var asks = new object[]
        {
            new { context = new { key = "u1" } },
            new { context = new { key = "vip-secret-user" } },
            new { context = new { key = "included-secret-user" } },
            new { context = new { key = "u1", attributes = new Dictionary<string, object> { ["internal-attribute"] = "hidden-value", ["segment-attribute"] = "segment-hidden-value" } } },
            new { flagKey = "beta-ui", context = new { key = "u1", attributes = new Dictionary<string, object> { ["segment-attribute"] = "segment-hidden-value" } } },
        };

        foreach (var ask in asks)
        {
            var response = await Post(client, ask);
            await ApiServer.Expect(HttpStatusCode.OK, response);
            var text = await response.Content.ReadAsStringAsync();
            foreach (var secret in secrets)
            {
                Assert.DoesNotContain(secret, text);
            }
            var flags = JsonDocument.Parse(text).RootElement.GetProperty("flags");
            foreach (var flag in flags.EnumerateObject())
            {
                Assert.Equal(["value", "variationIndex", "reason"], flag.Value.EnumerateObject().Select(p => p.Name));
                Assert.All(flag.Value.GetProperty("reason").EnumerateObject(), p => Assert.Contains(p.Name, new[] { "kind", "ruleIndex", "flagKey", "code" }));
            }
        }
    }

    [Fact]
    public async Task A_failed_prerequisite_names_the_prerequisite_flag_and_nothing_more()
    {
        await Seed("parent", "child");
        await Save("child", """
            { "on": true, "offVariation": 1, "targets": [], "prerequisites": [{ "flagKey": "parent", "variation": 0 }], "rules": [], "fallthrough": { "variation": 0 } }
            """);

        var body = await ApiServer.Json(await Post(await Key("client"), new { flagKey = "child", context = new { key = "u1" } }));

        Assert.Equal(
            """{"value":false,"variationIndex":1,"reason":{"kind":"PREREQUISITE_FAILED","flagKey":"parent"}}""",
            body.GetProperty("flags").GetProperty("child").GetRawText());
    }

    // --- CORS ----------------------------------------------------------------------------------------------

    private const string Origin = "https://app.example.com";

    private static string? AllowOrigin(HttpResponseMessage response) =>
        response.Headers.TryGetValues("Access-Control-Allow-Origin", out var values) ? string.Join(",", values) : null;

    [Fact]
    public async Task A_preflight_for_evaluate_is_answered_without_a_token()
    {
        await Seed();

        var response = await Call(HttpMethod.Options, Evaluate, null, null,
            ("Origin", Origin), ("Access-Control-Request-Method", "POST"), ("Access-Control-Request-Headers", "authorization,content-type"));

        Assert.Equal(HttpStatusCode.NoContent, response.StatusCode);
        Assert.Equal("*", AllowOrigin(response));
        Assert.Contains("POST", response.Headers.GetValues("Access-Control-Allow-Methods").Single());
        var allowed = response.Headers.GetValues("Access-Control-Allow-Headers").Single().ToLowerInvariant();
        Assert.Contains("authorization", allowed);
        Assert.Contains("content-type", allowed);
        Assert.Equal("600", response.Headers.GetValues("Access-Control-Max-Age").Single());
        // No cookies, so credentials are never allowed.
        Assert.False(response.Headers.Contains("Access-Control-Allow-Credentials"));
    }

    [Fact]
    public async Task An_evaluate_answer_carries_the_CORS_header_whatever_its_status()
    {
        await Seed("dark-mode");
        var client = await Key("client");

        var ok = await Post(client, new { context = new { key = "u1" } }, ("Origin", Origin));
        var unauthorized = await Post("not-a-key", new { context = new { key = "u1" } }, ("Origin", Origin));
        var bad = await Post(client, new { }, ("Origin", Origin));

        Assert.Equal(HttpStatusCode.OK, ok.StatusCode);
        Assert.Equal(HttpStatusCode.Unauthorized, unauthorized.StatusCode);
        Assert.Equal(HttpStatusCode.BadRequest, bad.StatusCode);
        Assert.All(new[] { ok, unauthorized, bad }, response => Assert.Equal("*", AllowOrigin(response)));
    }

    [Fact]
    public async Task Nothing_but_evaluate_has_CORS()
    {
        await Seed("dark-mode");
        var serverKey = await Key("server");
        var preflight = new[] { ("Origin", Origin), ("Access-Control-Request-Method", "GET"), ("Access-Control-Request-Headers", "authorization") };

        var responses = new[]
        {
            await Call(HttpMethod.Get, Ruleset, serverKey, null, ("Origin", Origin)),
            await Call(HttpMethod.Options, Ruleset, null, null, preflight),
            await Call(HttpMethod.Get, "/api/v1/projects", ApiServer.AdminToken, null, ("Origin", Origin)),
            await Call(HttpMethod.Options, "/api/v1/projects", null, null, preflight),
            await Call(HttpMethod.Get, "/health", null, null, ("Origin", Origin)),
            await Call(HttpMethod.Get, "/openapi/v1.json", null, null, ("Origin", Origin)),
        };

        Assert.All(responses, response => Assert.Null(AllowOrigin(response)));
        Assert.Equal(HttpStatusCode.OK, responses[0].StatusCode);
    }
}
