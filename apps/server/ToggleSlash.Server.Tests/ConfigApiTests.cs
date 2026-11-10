using System.Net.Http.Json;
using System.Net;
using System.Net.Http.Headers;
using System.Text.Json;

/// <summary>A flag's targeting in one environment: read, replace under a version, toggle, and the validation of what a save points at.</summary>
public sealed class ConfigApiTests : IDisposable
{
    private const string Flags = "/api/v1/projects/shop/flags";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private static string Config(string flag, string env = "prod", string project = "shop") =>
        $"/api/v1/projects/{project}/flags/{flag}/environments/{env}";

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

    /// <summary>The whole config a PUT replaces: the FS-11 defaults (off variation 1, fallthrough 0) unless a field is overridden by name and JSON.</summary>
    private static Dictionary<string, JsonElement> Body(long? version, params (string Field, string Json)[] overrides)
    {
        var body = new Dictionary<string, string>
        {
            ["on"] = "false",
            ["offVariation"] = "1",
            ["targets"] = "[]",
            ["prerequisites"] = "[]",
            ["rules"] = "[]",
            ["fallthrough"] = """{"variation":0}""",
        };
        if (version is not null)
        {
            body["version"] = version.Value.ToString();
        }
        foreach (var (field, json) in overrides)
        {
            body[field] = json;
        }
        return body.ToDictionary(pair => pair.Key, pair => JsonDocument.Parse(pair.Value).RootElement.Clone());
    }

    private Task<HttpResponseMessage> Put(string flag, Dictionary<string, JsonElement> body, string env = "prod") =>
        server.Put(Config(flag, env), body);

    private async Task<long> Version(string flag, string env = "prod") =>
        (await ApiServer.Json(await server.Get(Config(flag, env)))).GetProperty("version").GetInt64();

    private static Task ExpectBadRequest(HttpResponseMessage response) => ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", response);

    [Fact]
    public async Task A_new_flag_reads_back_with_the_FS_11_defaults_and_its_version_as_the_ETag()
    {
        await Seed("dark-mode");

        var response = await server.Get(Config("dark-mode"));

        await ApiServer.Expect(HttpStatusCode.OK, response);
        Assert.Equal("\"1\"", response.Headers.ETag!.Tag);
        var body = await ApiServer.Json(response);
        Assert.False(body.GetProperty("on").GetBoolean());
        Assert.Equal(1, body.GetProperty("offVariation").GetInt32());
        Assert.Equal("[]", body.GetProperty("targets").GetRawText());
        Assert.Equal("[]", body.GetProperty("prerequisites").GetRawText());
        Assert.Equal("[]", body.GetProperty("rules").GetRawText());
        Assert.Equal("""{"variation":0}""", body.GetProperty("fallthrough").GetRawText());
        Assert.Equal(1, body.GetProperty("version").GetInt64());
    }

    [Fact]
    public async Task The_defaults_are_valid_to_save_so_a_read_can_be_put_straight_back()
    {
        await Seed("dark-mode");
        var read = await ApiServer.Json(await server.Get(Config("dark-mode")));

        var put = await server.Put(Config("dark-mode"), read);

        await ApiServer.Expect(HttpStatusCode.OK, put);
        var body = await ApiServer.Json(put);
        Assert.Equal(2, body.GetProperty("version").GetInt64());
        Assert.Equal("\"2\"", put.Headers.ETag!.Tag);
    }

    [Theory]
    [InlineData("nope", "prod", "dark-mode")]
    [InlineData("shop", "nope", "dark-mode")]
    [InlineData("shop", "prod", "nope")]
    public async Task An_unknown_project_environment_or_flag_is_404(string project, string env, string flag)
    {
        await Seed("dark-mode");

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get(Config(flag, env, project)));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Put(Config(flag, env, project), Body(1)));
    }

    [Fact]
    public async Task Put_saves_every_field_and_reads_back_with_the_version_up_by_one()
    {
        await Seed("dark-mode", "base");
        var body = Body(1,
            ("on", "true"),
            ("offVariation", "null"),
            ("targets", """[{"variation":0,"contextKeys":["alice"]},{"variation":1,"contextKeys":["bob","carol"]}]"""),
            ("prerequisites", """[{"flagKey":"base","variation":0}]"""),
            ("rules", """[{"clauses":[{"attribute":"country","op":"in","values":["NL","BE"]}],"rollout":{"variations":[{"variation":0,"weight":25000},{"variation":1,"weight":75000}]}},{"clauses":[],"variation":1}]"""),
            ("fallthrough", """{"rollout":{"variations":[{"variation":0,"weight":100000}]}}"""));

        var put = await server.Put(Config("dark-mode"), body);

        await ApiServer.Expect(HttpStatusCode.OK, put);
        var saved = await ApiServer.Json(put);
        var read = await ApiServer.Json(await server.Get(Config("dark-mode")));
        Assert.Equal(saved.GetRawText(), read.GetRawText());
        Assert.True(read.GetProperty("on").GetBoolean());
        Assert.Equal(JsonValueKind.Null, read.GetProperty("offVariation").ValueKind);
        Assert.Equal(2, read.GetProperty("version").GetInt64());
        foreach (var field in new[] { "targets", "prerequisites", "rules", "fallthrough" })
        {
            Assert.Equal(body[field].GetRawText(), read.GetProperty(field).GetRawText());
        }
    }

    [Fact]
    public async Task A_config_belongs_to_its_environment()
    {
        await Seed("dark-mode");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("on", "true"))));

        Assert.Equal(2, await Version("dark-mode", "prod"));
        Assert.Equal(1, await Version("dark-mode", "staging"));
        Assert.Equal([("prod", true), ("staging", false)], server.Configs("dark-mode").Select(c => (c.Environment, c.On)));
    }

    [Fact]
    public async Task If_Match_can_carry_the_version_in_place_of_the_body_field()
    {
        await Seed("dark-mode");

        foreach (var (header, expected) in new[] { ("\"1\"", 2), ("W/\"2\"", 3), ("3", 4) })
        {
            using var client = server.Server.CreateClient();
            using var request = new HttpRequestMessage(HttpMethod.Put, Config("dark-mode"))
            {
                Content = JsonContent.Create(Body(null, ("on", "true"))),
            };
            request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", ApiServer.AdminToken);
            request.Headers.TryAddWithoutValidation("If-Match", header);

            var response = await client.SendAsync(request);

            await ApiServer.Expect(HttpStatusCode.OK, response);
            Assert.Equal(expected, (await ApiServer.Json(response)).GetProperty("version").GetInt64());
        }
    }

    [Fact]
    public async Task A_put_with_no_version_is_400()
    {
        await Seed("dark-mode");

        await ExpectBadRequest(await Put("dark-mode", Body(null)));
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Theory]
    [InlineData("\"x\"")]
    [InlineData("\"-1\"")]
    [InlineData("*")]
    public async Task An_If_Match_that_is_not_a_version_is_400(string header)
    {
        await Seed("dark-mode");
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Put, Config("dark-mode")) { Content = JsonContent.Create(Body(null)) };
        request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", ApiServer.AdminToken);
        request.Headers.TryAddWithoutValidation("If-Match", header);

        await ExpectBadRequest(await client.SendAsync(request));
    }

    [Fact]
    public async Task If_Match_and_the_body_version_must_agree()
    {
        await Seed("dark-mode");
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Put, Config("dark-mode")) { Content = JsonContent.Create(Body(1)) };
        request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", ApiServer.AdminToken);
        request.Headers.TryAddWithoutValidation("If-Match", "\"2\"");

        await ExpectBadRequest(await client.SendAsync(request));
    }

    [Fact]
    public async Task A_stale_version_is_409_and_changes_nothing()
    {
        await Seed("dark-mode");
        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("on", "true"))));

        var stale = await Put("dark-mode", Body(1, ("offVariation", "0")));

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", stale);
        var read = await ApiServer.Json(await server.Get(Config("dark-mode")));
        Assert.Equal(2, read.GetProperty("version").GetInt64());
        Assert.True(read.GetProperty("on").GetBoolean());
        Assert.Equal(1, read.GetProperty("offVariation").GetInt32());
    }

    [Fact]
    public async Task A_version_from_the_future_is_409_too()
    {
        await Seed("dark-mode");

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", await Put("dark-mode", Body(7)));
    }

    [Fact]
    public async Task Two_writers_on_the_same_version_get_one_save_and_one_409()
    {
        await Seed("dark-mode");

        var responses = await Task.WhenAll(Enumerable.Range(0, 8).Select(i =>
            Put("dark-mode", Body(1, ("targets", $$"""[{"variation":0,"contextKeys":["writer-{{i}}"]}]""")))));

        Assert.Equal(1, responses.Count(r => r.StatusCode == HttpStatusCode.OK));
        Assert.Equal(7, responses.Count(r => r.StatusCode == HttpStatusCode.Conflict));
        var read = await ApiServer.Json(await server.Get(Config("dark-mode")));
        Assert.Equal(2, read.GetProperty("version").GetInt64());
        var winner = await ApiServer.Json(responses.Single(r => r.StatusCode == HttpStatusCode.OK));
        Assert.Equal(winner.GetProperty("targets").GetRawText(), read.GetProperty("targets").GetRawText());
    }

    [Fact]
    public async Task Writers_that_retry_after_a_conflict_each_land_once()
    {
        await Seed("dark-mode");

        async Task Writer(int i)
        {
            for (var attempt = 0; attempt < 50; attempt++)
            {
                var version = await Version("dark-mode");
                var response = await Put("dark-mode", Body(version, ("targets", $$"""[{"variation":0,"contextKeys":["w{{i}}"]}]""")));
                if (response.StatusCode == HttpStatusCode.OK)
                {
                    return;
                }
                Assert.Equal(HttpStatusCode.Conflict, response.StatusCode);
            }
            Assert.Fail("never saved");
        }

        await Task.WhenAll(Enumerable.Range(0, 4).Select(Writer));

        Assert.Equal(5, await Version("dark-mode"));
    }

    [Fact]
    public async Task An_unknown_field_or_a_missing_one_is_400()
    {
        await Seed("dark-mode");

        var extra = Body(1, ("colour", "\"red\""));
        await ExpectBadRequest(await Put("dark-mode", extra));
        foreach (var field in new[] { "on", "targets", "prerequisites", "rules", "fallthrough" })
        {
            var body = Body(1);
            body.Remove(field);
            await ExpectBadRequest(await Put("dark-mode", body));
        }
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Fact]
    public async Task A_PUT_without_an_off_variation_serves_null_when_off()
    {
        await Seed("dark-mode");
        var body = Body(1);
        body.Remove("offVariation");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", body));

        Assert.Equal(JsonValueKind.Null, (await ApiServer.Json(await server.Get(Config("dark-mode")))).GetProperty("offVariation").ValueKind);
    }

    public static TheoryData<string, string, string> InvalidFields => new()
    {
        { "offVariation", "2", "an off variation past the last variation" },
        { "offVariation", "-1", "a negative off variation" },
        { "offVariation", "\"1\"", "an off variation as a string" },
        { "targets", """[{"variation":2,"contextKeys":["a"]}]""", "a target variation out of range" },
        { "targets", """[{"variation":-1,"contextKeys":["a"]}]""", "a negative target variation" },
        { "targets", """[{"variation":0,"contextKeys":[1]}]""", "a target key that is not a string" },
        { "targets", """[{"variation":0,"contextKeys":[""]}]""", "an empty target key" },
        { "targets", """[{"variation":0}]""", "a target with no keys" },
        { "targets", """[{"variation":0,"contextKeys":["a"],"x":1}]""", "an unknown field on a target" },
        { "targets", "{}", "targets that is not an array" },
        { "fallthrough", """{"variation":2}""", "a fallthrough variation out of range" },
        { "fallthrough", """{"variation":0.5}""", "a fractional fallthrough variation" },
        { "fallthrough", "{}", "a fallthrough that serves nothing" },
        { "fallthrough", """{"variation":0,"rollout":{"variations":[{"variation":0,"weight":100000}]}}""", "a fallthrough that serves two ways" },
        { "fallthrough", """{"rollout":{"variations":[{"variation":0,"weight":60000},{"variation":1,"weight":39999}]}}""", "rollout weights summing to 99999" },
        { "fallthrough", """{"rollout":{"variations":[{"variation":0,"weight":60000},{"variation":1,"weight":40001}]}}""", "rollout weights summing to 100001" },
        { "fallthrough", """{"rollout":{"variations":[]}}""", "an empty rollout" },
        { "fallthrough", """{"rollout":{"variations":[{"variation":5,"weight":100000}]}}""", "a rollout variation out of range" },
        { "fallthrough", """{"rollout":{"variations":[{"variation":0,"weight":-1},{"variation":1,"weight":100001}]}}""", "a negative weight" },
        { "fallthrough", """{"rollout":{"variations":[{"variation":0,"weight":99999.5},{"variation":1,"weight":0.5}]}}""", "fractional weights" },
        { "fallthrough", "null", "a null fallthrough" },
        { "fallthrough", """{"variation":0,"x":1}""", "an unknown field on the fallthrough" },
        { "rules", """[{"clauses":[],"variation":2}]""", "a rule variation out of range" },
        { "rules", """[{"clauses":[],"rollout":{"variations":[{"variation":0,"weight":1}]}}]""", "a rule rollout that does not sum to 100000" },
        { "rules", """[{"clauses":[]}]""", "a rule that serves nothing" },
        { "rules", """[{"variation":0}]""", "a rule with no clauses field" },
        { "rules", """[{"clauses":[{"attribute":"a","op":"regex","values":["x"]}],"variation":0}]""", "an unknown operator" },
        { "rules", """[{"clauses":[{"attribute":"","op":"in","values":["x"]}],"variation":0}]""", "an empty attribute" },
        { "rules", """[{"clauses":[{"attribute":"a","op":"in","values":[{}]}],"variation":0}]""", "a clause value that is an object" },
        { "rules", """[{"clauses":[{"attribute":"a","op":"in","values":["x"],"negate":"yes"}],"variation":0}]""", "a negate that is not a boolean" },
        { "rules", """[{"clauses":[{"op":"segmentMatch","values":[1]}],"variation":0}]""", "a segment key that is not a string" },
        { "prerequisites", """[{"flagKey":"base"}]""", "a prerequisite with no variation" },
        { "prerequisites", """[{"flagKey":"","variation":0}]""", "a prerequisite with an empty flag key" },
        { "prerequisites", """[{"flagKey":"base","variation":-1}]""", "a negative prerequisite variation" },
        { "prerequisites", """[{"flagKey":"base","variation":2}]""", "a prerequisite variation past the prerequisite's last" },
    };

    public static TheoryData<string> LoneSurrogates => new()
    {
        """{"on":false,"offVariation":1,"targets":[{"variation":0,"contextKeys":["\ud800"]}],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"version":1}""",
        """{"on":false,"offVariation":1,"targets":[],"prerequisites":[],"rules":[{"clauses":[{"attribute":"\ud800","op":"in","values":["x"]}],"variation":0}],"fallthrough":{"variation":0},"version":1}""",
        """{"on":false,"offVariation":1,"targets":[],"prerequisites":[],"rules":[{"clauses":[],"variation":0,"\ud800":1}],"fallthrough":{"variation":0},"version":1}""",
    };

    /// <summary>A v prefix, a leading zero, an Arabic-Indic digit: the parser has its own vectors.</summary>
    public static TheoryData<string> BadSemverValues => ["v1", "01.2.3", "1.\u0661.0"];

    /// <summary>A number and a null: not strings.</summary>
    public static TheoryData<string> BadSemverTypes => ["2", "null"];

    private static string SemverRule(string valuesJson, string op = "semverGte") =>
        $$"""[{"clauses":[{"attribute":"version","op":"{{op}}","values":{{valuesJson}}}],"variation":0}]""";

    [Theory]
    [MemberData(nameof(BadSemverValues))]
    public async Task A_semver_clause_with_a_value_that_is_not_a_version_is_400_naming_the_value(string value)
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, ("rules", SemverRule($"[\"1.0.0\",{JsonSerializer.Serialize(value)}]"))));

        await ExpectBadRequest(response);
        Assert.Contains("rules[0].clauses[0].values[1]", (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Theory]
    [MemberData(nameof(BadSemverTypes))]
    public async Task A_semver_clause_with_a_value_that_is_not_a_string_is_400(string valueJson)
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, ("rules", SemverRule($"[{valueJson}]"))));

        await ExpectBadRequest(response);
        Assert.Contains("rules[0].clauses[0].values[0]", (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
    }

    [Fact]
    public async Task A_number_value_stays_valid_under_the_other_ops()
    {
        await Seed("dark-mode", "base");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("rules", SemverRule("[2]", "gte")))));
    }

    [Theory]
    [InlineData("semverEq")]
    [InlineData("semverLt")]
    [InlineData("semverLte")]
    [InlineData("semverGt")]
    [InlineData("semverGte")]
    public async Task A_semver_clause_with_versions_saves_and_the_ruleset_carries_it(string op)
    {
        await Seed("dark-mode", "base");
        var rules = SemverRule("[\"2.10\",\"1.2.3-rc.1+build.5\"]", op);

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("rules", rules))));

        var read = await ApiServer.Json(await server.Get(Config("dark-mode")));
        Assert.Equal(JsonDocument.Parse(rules).RootElement.GetRawText(), read.GetProperty("rules").GetRawText());
        var key = await ApiServer.Json(await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind = "server", name = "s" }));
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Get, "/sdk/v1/ruleset");
        request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", key.GetProperty("key").GetString());
        var ruleset = await client.SendAsync(request);
        await ApiServer.Expect(HttpStatusCode.OK, ruleset);
        var flag = (await ApiServer.Json(ruleset)).GetProperty("flags").EnumerateArray().Single(f => f.GetProperty("key").GetString() == "dark-mode");
        var clause = flag.GetProperty("rules")[0].GetProperty("clauses")[0];
        Assert.Equal(op, clause.GetProperty("op").GetString());
        Assert.Equal("1.2.3-rc.1+build.5", clause.GetProperty("values")[1].GetString());
    }

    [Fact]
    public async Task A_semver_clause_with_no_values_saves()
    {
        await Seed("dark-mode", "base");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("rules", SemverRule("[]")))));
    }

    /// <summary>An unmatched group, a bare quantifier, an escape the dialect lacks, a bare ]: the parser has its own vectors.</summary>
    public static TheoryData<string> BadPatterns => ["(", "*a", "\\b", "a]"];

    private static string MatchesRule(string valuesJson) => SemverRule(valuesJson, "matches");

    [Theory]
    [MemberData(nameof(BadPatterns))]
    public async Task A_matches_clause_with_a_value_that_is_not_a_pattern_is_400_naming_the_value(string value)
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, ("rules", MatchesRule($"[\"a\",{JsonSerializer.Serialize(value)}]"))));

        await ExpectBadRequest(response);
        var message = (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString();
        Assert.Contains("rules[0].clauses[0].values[1]", message);
        Assert.Contains("not a valid pattern", message);
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Fact]
    public async Task A_matches_clause_with_a_value_that_is_not_a_string_is_400()
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, ("rules", MatchesRule("[42]"))));

        await ExpectBadRequest(response);
        Assert.Contains("rules[0].clauses[0].values[0]", (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
    }

    [Fact]
    public async Task A_matches_clause_with_patterns_saves_and_reads_back()
    {
        await Seed("dark-mode", "base");
        var rules = MatchesRule("[\"^\\\\d+$\",\"a{1,3}\"]");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("rules", rules))));

        var read = await ApiServer.Json(await server.Get(Config("dark-mode")));
        Assert.Equal(JsonDocument.Parse(rules).RootElement.GetRawText(), read.GetProperty("rules").GetRawText());
    }

    [Fact]
    public async Task A_matches_clause_with_no_values_saves()
    {
        await Seed("dark-mode", "base");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("rules", MatchesRule("[]")))));
    }

    /// <summary>Not instants: no offset, no seconds, lowercase, a space, a bare date, a bad day, an Arabic-Indic digit.</summary>
    public static TheoryData<string> BadDateStrings =>
    [
        "2026-01-01T00:00:00",
        "2026-01-01T00:00Z",
        "2026-01-01t00:00:00z",
        "2026-01-01 00:00:00Z",
        "2026-01-01",
        "2023-02-29T00:00:00Z",
        "2026-01-01T00:00:00\u0661Z",
    ];

    private static string DateRule(string valuesJson, string op = "before") =>
        $$"""[{"clauses":[{"attribute":"signedUp","op":"{{op}}","values":{{valuesJson}}}],"variation":0}]""";

    [Theory]
    [MemberData(nameof(BadDateStrings))]
    public async Task A_date_clause_with_a_string_that_is_not_an_instant_is_400_naming_the_value(string value)
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, ("rules", DateRule($"[\"2026-01-01T00:00:00Z\",{JsonSerializer.Serialize(value)}]"))));

        await ExpectBadRequest(response);
        Assert.Contains("rules[0].clauses[0].values[1]", (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Theory]
    [InlineData("true")]
    [InlineData("null")]
    [InlineData("{}")]
    public async Task A_date_clause_with_a_value_that_is_not_a_string_or_number_is_400(string valueJson)
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, ("rules", DateRule($"[{valueJson}]", "after"))));

        await ExpectBadRequest(response);
        Assert.Contains("rules[0].clauses[0].values[0]", (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
    }

    [Fact]
    public async Task A_date_clause_with_a_number_that_is_not_finite_is_400()
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, ("rules", DateRule("[1e999]"))));

        await ExpectBadRequest(response);
        Assert.Contains("rules[0].clauses[0].values[0]", (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
    }

    [Theory]
    [InlineData("before")]
    [InlineData("after")]
    public async Task A_date_clause_with_a_string_and_a_number_saves_and_the_ruleset_carries_it(string op)
    {
        await Seed("dark-mode", "base");
        var rules = DateRule("[\"2026-01-01T00:00:00Z\",1767225600000,\"2026-01-01T02:00:00.5+02:00\"]", op);

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("rules", rules))));

        var read = await ApiServer.Json(await server.Get(Config("dark-mode")));
        Assert.Equal(JsonDocument.Parse(rules).RootElement.GetRawText(), read.GetProperty("rules").GetRawText());
        var key = await ApiServer.Json(await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind = "server", name = "s" }));
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Get, "/sdk/v1/ruleset");
        request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", key.GetProperty("key").GetString());
        var ruleset = await client.SendAsync(request);
        await ApiServer.Expect(HttpStatusCode.OK, ruleset);
        var flag = (await ApiServer.Json(ruleset)).GetProperty("flags").EnumerateArray().Single(f => f.GetProperty("key").GetString() == "dark-mode");
        var clause = flag.GetProperty("rules")[0].GetProperty("clauses")[0];
        Assert.Equal(op, clause.GetProperty("op").GetString());
        Assert.Equal("2026-01-01T00:00:00Z", clause.GetProperty("values")[0].GetString());
        Assert.Equal(1767225600000d, clause.GetProperty("values")[1].GetDouble());
    }

    [Fact]
    public async Task A_target_clause_and_rollout_with_a_context_kind_save_and_read_back()
    {
        await Seed("dark-mode", "base");
        var targets = """[{"variation":0,"contextKeys":["acme"],"contextKind":"org"}]""";
        var rules = """[{"clauses":[{"attribute":"seats","op":"gt","values":[10],"contextKind":"org"},{"op":"segmentMatch","values":[],"contextKind":"ignored here"}],"variation":0}]""";
        var fallthrough = """{"rollout":{"variations":[{"variation":0,"weight":100000}],"contextKind":"device.v2_x-1"}}""";

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("targets", targets), ("rules", rules), ("fallthrough", fallthrough))));

        var read = await ApiServer.Json(await server.Get(Config("dark-mode")));
        Assert.Equal(targets, read.GetProperty("targets").GetRawText());
        Assert.Equal(rules, read.GetProperty("rules").GetRawText());
        Assert.Equal(fallthrough, read.GetProperty("fallthrough").GetRawText());
    }

    [Fact]
    public async Task A_null_context_kind_is_the_same_as_none()
    {
        await Seed("dark-mode", "base");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("targets", """[{"variation":0,"contextKeys":["u1"],"contextKind":null}]"""))));
    }

    [Theory]
    [InlineData("targets", """[{"variation":0,"contextKeys":["acme"],"contextKind":"multi"}]""", "targets[0].contextKind")]
    [InlineData("rules", """[{"clauses":[{"attribute":"a","op":"in","values":[],"contextKind":"a b"}],"variation":0}]""", "rules[0].clauses[0].contextKind")]
    [InlineData("rules", """[{"clauses":[],"rollout":{"variations":[{"variation":0,"weight":100000}],"contextKind":7}}]""", "rules[0].rollout.contextKind")]
    [InlineData("fallthrough", """{"rollout":{"variations":[{"variation":0,"weight":100000}],"contextKind":"org:x"}}""", "fallthrough.rollout.contextKind")]
    public async Task A_context_kind_that_is_not_a_kind_is_400_naming_the_path(string field, string json, string path)
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, (field, json)));

        await ExpectBadRequest(response);
        Assert.Equal(
            $"{path} must match [A-Za-z0-9._-]+ and not be \"kind\" or \"multi\".",
            (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
        Assert.Equal(1, await Version("dark-mode"));
    }

    private static string ReferenceRule(string attribute) =>
        $$"""[{"clauses":[{"attribute":{{JsonSerializer.Serialize(attribute)}},"op":"in","values":["Paris"]}],"variation":0}]""";

    [Fact]
    public async Task An_attribute_reference_saves_and_is_read_back_as_written()
    {
        await Seed("dark-mode", "base");
        var rules = ReferenceRule("/address/city");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("rules", rules))));

        var read = await ApiServer.Json(await server.Get(Config("dark-mode")));
        Assert.Equal("/address/city", read.GetProperty("rules")[0].GetProperty("clauses")[0].GetProperty("attribute").GetString());
    }

    [Theory]
    [InlineData("/a//b")]
    [InlineData("/a~2")]
    public async Task An_attribute_reference_that_is_not_valid_is_400_naming_the_field(string attribute)
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, ("rules", ReferenceRule(attribute))));

        await ExpectBadRequest(response);
        Assert.StartsWith("rules[0].clauses[0].attribute is not a valid attribute reference.", (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Fact]
    public async Task An_attribute_name_with_a_tilde_and_no_leading_slash_saves()
    {
        await Seed("dark-mode", "base");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("rules", ReferenceRule("a~2")))));
    }

    [Fact]
    public async Task A_date_clause_with_no_values_saves()
    {
        await Seed("dark-mode", "base");

        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("rules", DateRule("[]")))));
    }

    [Theory]
    [MemberData(nameof(LoneSurrogates))]
    public async Task A_lone_surrogate_is_400_not_500(string json)
    {
        await Seed("dark-mode", "base");
        using var client = server.Server.CreateClient();
        client.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", ApiServer.AdminToken);

        var response = await client.PutAsync(Config("dark-mode"), new StringContent(json, System.Text.Encoding.UTF8, "application/json"));

        await ExpectBadRequest(response);
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Theory]
    [MemberData(nameof(InvalidFields))]
    public async Task An_invalid_field_is_400_and_saves_nothing(string field, string json, string because)
    {
        await Seed("dark-mode", "base");

        var response = await Put("dark-mode", Body(1, (field, json)));

        Assert.True(response.StatusCode == HttpStatusCode.BadRequest, because + ": " + await response.Content.ReadAsStringAsync());
        await ExpectBadRequest(response);
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Fact]
    public async Task Rollout_weights_that_sum_to_exactly_100000_save_including_a_zero_and_a_repeat()
    {
        await Seed("dark-mode");

        var put = await Put("dark-mode", Body(1, ("fallthrough", """{"rollout":{"variations":[{"variation":0,"weight":0},{"variation":1,"weight":99999},{"variation":0,"weight":1}]}}""")));

        await ApiServer.Expect(HttpStatusCode.OK, put);
    }

    [Fact]
    public async Task The_off_variation_and_targets_are_checked_against_the_flags_own_variations()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedFlag("shop", "color", ["red", "green", "blue"]);

        await ApiServer.Expect(HttpStatusCode.OK, await Put("color", Body(1, ("offVariation", "2"), ("targets", """[{"variation":2,"contextKeys":["a"]}]"""), ("fallthrough", """{"variation":2}"""))));
        await ExpectBadRequest(await Put("color", Body(2, ("offVariation", "3"))));
    }

    [Fact]
    public async Task A_prerequisite_must_name_a_flag_that_exists()
    {
        await Seed("dark-mode");

        var response = await Put("dark-mode", Body(1, ("prerequisites", """[{"flagKey":"ghost","variation":0}]""")));

        await ExpectBadRequest(response);
    }

    [Fact]
    public async Task A_flag_cannot_be_its_own_prerequisite()
    {
        await Seed("dark-mode");

        await ExpectBadRequest(await Put("dark-mode", Body(1, ("prerequisites", """[{"flagKey":"dark-mode","variation":0}]"""))));
    }

    [Fact]
    public async Task A_prerequisite_in_another_project_is_400_even_when_the_key_exists_there()
    {
        await Seed("dark-mode");
        await server.SeedProject("other");
        await server.SeedEnvironment("other", "prod");
        await server.SeedFlag("other", "elsewhere");

        var response = await Put("dark-mode", Body(1, ("prerequisites", """[{"flagKey":"elsewhere","variation":0}]""")));

        await ExpectBadRequest(response);
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Fact]
    public async Task A_segment_that_does_not_exist_is_400()
    {
        await Seed("dark-mode");

        await ExpectBadRequest(await Put("dark-mode", Body(1, ("rules", """[{"clauses":[{"op":"segmentMatch","values":["ghost"]}],"variation":0}]"""))));
    }

    [Fact]
    public async Task A_segment_of_another_environment_is_400_and_one_of_the_same_environment_saves()
    {
        await Seed("dark-mode");
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects/shop/environments/staging/segments", new { key = "beta", included = new[] { "a" }, excluded = Array.Empty<string>(), rules = Array.Empty<object>() }));
        var rules = ("rules", """[{"clauses":[{"op":"segmentMatch","values":["beta"]}],"variation":0}]""");

        await ExpectBadRequest(await Put("dark-mode", Body(1, rules)));
        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, rules), "staging"));
    }

    [Fact]
    public async Task A_segment_of_another_project_is_400()
    {
        await Seed("dark-mode");
        await server.SeedProject("other");
        await server.SeedEnvironment("other", "prod");
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects/other/environments/prod/segments", new { key = "beta", included = Array.Empty<string>(), excluded = Array.Empty<string>(), rules = Array.Empty<object>() }));

        await ExpectBadRequest(await Put("dark-mode", Body(1, ("rules", """[{"clauses":[{"op":"segmentMatch","values":["beta"]}],"variation":0}]"""))));
    }

    [Fact]
    public async Task Prerequisites_that_would_make_a_cycle_are_400()
    {
        await Seed("a", "b", "c");
        await ApiServer.Expect(HttpStatusCode.OK, await Put("a", Body(1, ("prerequisites", """[{"flagKey":"b","variation":0}]"""))));
        await ApiServer.Expect(HttpStatusCode.OK, await Put("b", Body(1, ("prerequisites", """[{"flagKey":"c","variation":0}]"""))));

        var direct = await Put("b", Body(2, ("prerequisites", """[{"flagKey":"a","variation":0}]""")));
        var indirect = await Put("c", Body(1, ("prerequisites", """[{"flagKey":"a","variation":0}]""")));

        await ExpectBadRequest(direct);
        await ExpectBadRequest(indirect);
        Assert.Contains("cycle", (await ApiServer.Json(indirect)).GetProperty("error").GetProperty("message").GetString());
        Assert.Equal(1, await Version("c"));
    }

    [Fact]
    public async Task A_cycle_in_another_environment_is_not_a_cycle_here()
    {
        await Seed("a", "b");
        await ApiServer.Expect(HttpStatusCode.OK, await Put("a", Body(1, ("prerequisites", """[{"flagKey":"b","variation":0}]""")), "staging"));

        await ApiServer.Expect(HttpStatusCode.OK, await Put("b", Body(1, ("prerequisites", """[{"flagKey":"a","variation":0}]""")), "prod"));
    }

    [Fact]
    public async Task A_diamond_of_prerequisites_is_not_a_cycle()
    {
        await Seed("top", "left", "right", "base");
        await ApiServer.Expect(HttpStatusCode.OK, await Put("left", Body(1, ("prerequisites", """[{"flagKey":"base","variation":0}]"""))));
        await ApiServer.Expect(HttpStatusCode.OK, await Put("right", Body(1, ("prerequisites", """[{"flagKey":"base","variation":0}]"""))));

        await ApiServer.Expect(HttpStatusCode.OK, await Put("top", Body(1, ("prerequisites", """[{"flagKey":"left","variation":0},{"flagKey":"right","variation":0}]"""))));
    }

    [Fact]
    public async Task Toggle_flips_on_and_nothing_else_and_bumps_the_version()
    {
        await Seed("dark-mode");
        var before = await ApiServer.Json(await server.Get(Config("dark-mode")));

        var response = await server.Post(Config("dark-mode") + "/toggle", new { on = true });

        await ApiServer.Expect(HttpStatusCode.OK, response);
        var after = await ApiServer.Json(response);
        Assert.True(after.GetProperty("on").GetBoolean());
        Assert.Equal(2, after.GetProperty("version").GetInt64());
        Assert.Equal("\"2\"", response.Headers.ETag!.Tag);
        foreach (var field in new[] { "offVariation", "targets", "prerequisites", "rules", "fallthrough" })
        {
            Assert.Equal(before.GetProperty(field).GetRawText(), after.GetProperty(field).GetRawText());
        }
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post(Config("dark-mode") + "/toggle", new { on = false }));
        Assert.False((await ApiServer.Json(await server.Get(Config("dark-mode")))).GetProperty("on").GetBoolean());
    }

    [Fact]
    public async Task Toggle_with_a_version_is_409_when_it_is_stale_and_without_one_applies_to_the_current()
    {
        await Seed("dark-mode");
        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("offVariation", "0"))));

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", await server.Post(Config("dark-mode") + "/toggle", new { on = true, version = 1 }));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post(Config("dark-mode") + "/toggle", new { on = true, version = 2 }));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post(Config("dark-mode") + "/toggle", new { on = false }));
        Assert.Equal(4, await Version("dark-mode"));
    }

    [Fact]
    public async Task A_toggle_makes_a_PUT_read_before_it_stale()
    {
        await Seed("dark-mode");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post(Config("dark-mode") + "/toggle", new { on = true }));

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", await Put("dark-mode", Body(1)));
    }

    [Theory]
    [InlineData("{}")]
    [InlineData("""{"on":"yes"}""")]
    [InlineData("""{"on":true,"extra":1}""")]
    [InlineData("""{"on":true,"version":-1}""")]
    public async Task A_toggle_needs_a_boolean_on(string json)
    {
        await Seed("dark-mode");
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Post, Config("dark-mode") + "/toggle")
        {
            Content = new StringContent(json, System.Text.Encoding.UTF8, "application/json"),
        };
        request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", ApiServer.AdminToken);

        await ExpectBadRequest(await client.SendAsync(request));
        Assert.Equal(1, await Version("dark-mode"));
    }

    [Fact]
    public async Task A_new_environment_after_a_save_still_starts_at_version_1_with_the_defaults()
    {
        await Seed("dark-mode");
        await ApiServer.Expect(HttpStatusCode.OK, await Put("dark-mode", Body(1, ("on", "true"))));
        await server.SeedEnvironment("shop", "qa");

        var read = await ApiServer.Json(await server.Get(Config("dark-mode", "qa")));

        Assert.Equal(1, read.GetProperty("version").GetInt64());
        Assert.False(read.GetProperty("on").GetBoolean());
    }
}
