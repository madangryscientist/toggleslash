using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.AspNetCore.Hosting;
using Microsoft.Extensions.Logging;

/// <summary>
/// The admin diagnostic route, <c>POST /api/v1/projects/{project}/environments/{env}/evaluate</c>: it evaluates like
/// <c>POST /sdk/v1/evaluate</c>, for an admin, and records nothing.
/// </summary>
public sealed class DiagnoseApiTests : IDisposable
{
    private const string Diagnose = "/api/v1/projects/shop/environments/prod/evaluate";
    private const string SdkEvaluate = "/sdk/v1/evaluate";

    private readonly CapturedLogs logs = new();
    private readonly ApiServer server;

    public DiagnoseApiTests() =>
        server = ApiServer.With(builder => builder.ConfigureLogging(logging => logging.SetMinimumLevel(LogLevel.Trace).AddProvider(logs)));

    public void Dispose() => server.Dispose();

    private const string Targeted = """
        { "on": true, "offVariation": 1,
          "targets": [{ "variation": 1, "contextKeys": ["vip-secret-user"] }],
          "prerequisites": [],
          "rules": [{ "clauses": [{ "attribute": "internal-attribute", "op": "in", "values": ["hidden-value"] }], "variation": 1 }],
          "fallthrough": { "variation": 0 } }
        """;

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("shop", "staging");
        foreach (var flag in new[] { "dark-mode", "beta-ui", "old-flag" })
        {
            await server.SeedFlag("shop", flag);
        }
        await Save("dark-mode", Targeted);
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/beta-ui/environments/prod/toggle", new { on = true }));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/old-flag/archive"));
    }

    private async Task Save(string flag, string configJson)
    {
        var path = $"/api/v1/projects/shop/flags/{flag}/environments/prod";
        var version = (await ApiServer.Json(await server.Get(path))).GetProperty("version").GetInt64();
        var config = JsonNode.Parse(configJson)!.AsObject();
        config["version"] = version;
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put(path, JsonDocument.Parse(config.ToJsonString()).RootElement));
    }

    private async Task<string> Key(string kind)
    {
        var created = await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private Task<HttpResponseMessage> Admin(object body, string path = Diagnose) => server.Post(path, body);

    private Task<HttpResponseMessage> Sdk(string token, object body) =>
        server.Send(HttpMethod.Post, SdkEvaluate, $"Bearer {token}", body);

    private async Task<HttpResponseMessage> Raw(string path, string? authorization, string json)
    {
        using var client = server.Server.CreateClient();
        var request = new HttpRequestMessage(HttpMethod.Post, path)
        {
            Content = new StringContent(json, new MediaTypeHeaderValue("application/json")),
        };
        if (authorization is not null)
        {
            request.Headers.TryAddWithoutValidation("Authorization", authorization);
        }
        return await client.SendAsync(request);
    }

    private long UsageRows()
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT COUNT(*) FROM flag_usage_daily";
        return (long)command.ExecuteScalar()!;
    }

    private static readonly object[] Contexts =
    [
        new { key = "u1" },
        new { key = "vip-secret-user" },
        new { key = "u2", attributes = new Dictionary<string, object> { ["internal-attribute"] = "hidden-value" } },
    ];

    // --- agreement with the SDK route ----------------------------------------------------------------------

    [Fact]
    public async Task It_agrees_with_the_SDK_route_for_every_flag()
    {
        await Seed();
        var client = await Key("client");

        foreach (var context in Contexts)
        {
            var admin = await Admin(new { context });
            var sdk = await Sdk(client, new { context });
            await ApiServer.Expect(HttpStatusCode.OK, admin);
            await ApiServer.Expect(HttpStatusCode.OK, sdk);
            var diagnosed = (await ApiServer.Json(admin)).GetProperty("flags");
            var served = (await ApiServer.Json(sdk)).GetProperty("flags");

            Assert.Equal(["beta-ui", "dark-mode"], diagnosed.EnumerateObject().Select(p => p.Name));
            Assert.Equal(served.EnumerateObject().Select(p => p.Name), diagnosed.EnumerateObject().Select(p => p.Name));
            foreach (var flag in served.EnumerateObject())
            {
                Same(flag.Value, diagnosed.GetProperty(flag.Name));
            }
        }
    }

    [Fact]
    public async Task It_agrees_with_the_SDK_route_for_one_flag()
    {
        await Seed();
        var client = await Key("server");

        foreach (var context in Contexts)
        {
            var admin = await ApiServer.Json(await Admin(new { flagKey = "dark-mode", context }));
            var sdk = await ApiServer.Json(await Sdk(client, new { flagKey = "dark-mode", context }));

            var only = Assert.Single(admin.GetProperty("flags").EnumerateObject());
            Assert.Equal("dark-mode", only.Name);
            Same(sdk.GetProperty("flags").GetProperty("dark-mode"), only.Value);
        }
    }

    private static void Same(JsonElement expected, JsonElement actual)
    {
        Assert.Equal(expected.GetProperty("value").GetRawText(), actual.GetProperty("value").GetRawText());
        Assert.Equal(expected.GetProperty("variationIndex").GetRawText(), actual.GetProperty("variationIndex").GetRawText());
        Assert.Equal(expected.GetProperty("reason").GetRawText(), actual.GetProperty("reason").GetRawText());
    }

    [Fact]
    public async Task It_names_the_rule_that_matched()
    {
        await Seed();

        var body = await ApiServer.Json(await Admin(new
        {
            flagKey = "dark-mode",
            context = new { key = "u1", attributes = new Dictionary<string, object> { ["internal-attribute"] = "hidden-value" } },
        }));

        Assert.Equal(
            """{"value":false,"variationIndex":1,"reason":{"kind":"RULE_MATCH","ruleIndex":0}}""",
            body.GetProperty("flags").GetProperty("dark-mode").GetRawText());
    }

    [Fact]
    public async Task It_serves_the_environment_in_the_path_not_one_an_SDK_key_belongs_to()
    {
        await Seed();

        var prod = await ApiServer.Json(await Admin(new { flagKey = "dark-mode", context = new { key = "u1" } }));
        var staging = await ApiServer.Json(await Admin(
            new { flagKey = "dark-mode", context = new { key = "u1" } }, "/api/v1/projects/shop/environments/staging/evaluate"));

        Assert.Equal("FALLTHROUGH", prod.GetProperty("flags").GetProperty("dark-mode").GetProperty("reason").GetProperty("kind").GetString());
        Assert.Equal("OFF", staging.GetProperty("flags").GetProperty("dark-mode").GetProperty("reason").GetProperty("kind").GetString());
    }

    // --- at a past hash ------------------------------------------------------------------------------------

    private async Task<string> ServeRuleset(string env = "prod")
    {
        var response = await server.Send(HttpMethod.Get, "/sdk/v1/ruleset", $"Bearer {await KeyIn(env, "server")}");
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return response.Headers.ETag!.Tag.Trim('"');
    }

    private async Task<string> KeyIn(string env, string kind)
    {
        var created = await server.Post($"/api/v1/projects/shop/environments/{env}/keys", new { kind, name = $"{kind}-{env}" });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    [Fact]
    public async Task With_an_old_etag_it_gives_the_old_answer_and_reason_and_without_one_the_new()
    {
        await Seed();
        var context = new { key = "u1" };
        var oldHash = await ServeRuleset();
        var before = await ApiServer.Json(await Admin(new { flagKey = "beta-ui", context }));
        Assert.Equal("""{"value":true,"variationIndex":0,"reason":{"kind":"FALLTHROUGH"}}""", before.GetProperty("flags").GetProperty("beta-ui").GetRawText());

        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/beta-ui/environments/prod/toggle", new { on = false }));
        var newHash = await ServeRuleset();
        Assert.NotEqual(oldHash, newHash);

        var old = await ApiServer.Json(await Admin(new { flagKey = "beta-ui", context, etag = oldHash }));
        var current = await ApiServer.Json(await Admin(new { flagKey = "beta-ui", context }));
        var atNew = await ApiServer.Json(await Admin(new { flagKey = "beta-ui", context, etag = newHash }));

        Assert.Equal(before.GetRawText(), old.GetRawText());
        Assert.Equal("OFF", current.GetProperty("flags").GetProperty("beta-ui").GetProperty("reason").GetProperty("kind").GetString());
        Assert.Equal(current.GetRawText(), atNew.GetRawText());
    }

    [Fact]
    public async Task With_an_etag_and_no_flag_it_answers_every_flag_the_old_ruleset_had()
    {
        await Seed();
        var oldHash = await ServeRuleset();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post("/api/v1/projects/shop/flags/dark-mode/archive"));

        var old = await ApiServer.Json(await Admin(new { context = new { key = "u1" }, etag = oldHash }));
        var current = await ApiServer.Json(await Admin(new { context = new { key = "u1" } }));

        Assert.Equal(["beta-ui", "dark-mode"], old.GetProperty("flags").EnumerateObject().Select(p => p.Name));
        Assert.Equal(["beta-ui"], current.GetProperty("flags").EnumerateObject().Select(p => p.Name));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await Admin(new { flagKey = "old-flag", context = new { key = "u1" }, etag = oldHash }));
    }

    [Fact]
    public async Task A_hash_never_served_or_from_another_environment_is_404_and_a_bad_context_is_still_400()
    {
        await Seed();
        var staging = await ServeRuleset("staging");
        var prod = await ServeRuleset();
        var context = new { key = "u1" };

        var neverServed = await Admin(new { context, etag = new string('0', 64) });
        var other = await Admin(new { context, etag = staging });

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", neverServed);
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", other);
        Assert.Contains("never served a ruleset", (await ApiServer.Json(neverServed)).GetProperty("error").GetProperty("message").GetString());
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await Admin(new { context, etag = $"\"{prod}\"" }));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await Admin(new { context, etag = "" }));
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await Admin(new { context = new { key = "" }, etag = prod }));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await Admin(new { flagKey = "nope", context, etag = prod }));
    }

    [Fact]
    public async Task A_read_token_may_use_an_etag_and_the_SDK_route_still_refuses_the_field()
    {
        await Seed();
        var hash = await ServeRuleset();
        var token = (await ApiServer.Json(await server.Post("/api/v1/projects/shop/tokens", new { name = "r", permission = "read", expiresInDays = 30 })))
            .GetProperty("token").GetString()!;
        var body = new { context = new { key = "u1" }, etag = hash };

        var read = await server.Send(HttpMethod.Post, Diagnose, $"Bearer {token}", body);
        var sdk = await Sdk(await Key("client"), body);

        await ApiServer.Expect(HttpStatusCode.OK, read);
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", sdk);
    }

    [Fact]
    public async Task An_etag_answer_records_nothing_and_carries_no_rule_data_or_salt()
    {
        await Seed();
        var hash = await ServeRuleset();
        var audit = server.AuditRows().Count;
        var snapshots = Snapshots();

        var response = await Admin(new { context = new { key = "vip-secret-user" }, etag = hash });
        await ApiServer.Expect(HttpStatusCode.OK, response);
        var text = await response.Content.ReadAsStringAsync();

        Assert.Equal(0, UsageRows());
        Assert.Equal(audit, server.AuditRows().Count);
        Assert.Equal(snapshots, Snapshots());
        foreach (var secret in new[] { "vip-secret-user", "internal-attribute", "hidden-value", server.Salt("flags", "shop", "dark-mode"), "targets", "rules", "clauses", "salt" })
        {
            Assert.DoesNotContain(secret, text);
        }
    }

    private long Snapshots()
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT COUNT(*) FROM ruleset_snapshots";
        return (long)command.ExecuteScalar()!;
    }

    // --- it records nothing --------------------------------------------------------------------------------

    [Fact]
    public async Task It_counts_no_usage_and_writes_no_audit_event_while_the_SDK_route_still_counts()
    {
        await Seed();
        var audit = server.AuditRows().Count;

        foreach (var context in Contexts)
        {
            await ApiServer.Expect(HttpStatusCode.OK, await Admin(new { context }));
            await ApiServer.Expect(HttpStatusCode.OK, await Admin(new { flagKey = "dark-mode", context }));
        }

        Assert.Equal(0, UsageRows());
        Assert.Equal(audit, server.AuditRows().Count);

        await ApiServer.Expect(HttpStatusCode.OK, await Sdk(await Key("client"), new { context = new { key = "u1" } }));

        Assert.True(UsageRows() > 0);
        Assert.Equal(audit + 1, server.AuditRows().Count); // the audit event of creating that key, nothing from evaluating
    }

    [Fact]
    public async Task It_does_not_store_or_log_the_context()
    {
        await Seed();
        var context = new { key = "diagnosed-person-9917", attributes = new Dictionary<string, object> { ["email"] = "private-9917@example.test" } };

        await ApiServer.Expect(HttpStatusCode.OK, await Admin(new { context }));
        await ApiServer.Expect(HttpStatusCode.OK, await Admin(new { flagKey = "dark-mode", context }));
        await ApiServer.Expect(HttpStatusCode.BadRequest, await Admin(new { flagKey = "dark-mode", context = new { key = "", attributes = "private-9917@example.test" } }));
        await ApiServer.Expect(HttpStatusCode.NotFound, await Admin(new { flagKey = "nope-9917", context }));

        Assert.DoesNotContain("diagnosed-person-9917", logs.All);
        Assert.DoesNotContain("private-9917", logs.All);
        Assert.DoesNotContain("diagnosed-person-9917", string.Join('\n', server.AuditRows().Select(r => r.Everything)));
        using var connection = server.OpenDatabase();
        using var tables = connection.CreateCommand();
        tables.CommandText = "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'";
        using var names = tables.ExecuteReader();
        var all = new List<string>();
        while (names.Read())
        {
            all.Add(names.GetString(0));
        }
        names.Close();
        foreach (var table in all)
        {
            using var dump = connection.CreateCommand();
            dump.CommandText = $"SELECT * FROM \"{table}\"";
            using var rows = dump.ExecuteReader();
            while (rows.Read())
            {
                for (var i = 0; i < rows.FieldCount; i++)
                {
                    var text = rows.IsDBNull(i) ? "" : Convert.ToString(rows.GetValue(i)) ?? "";
                    Assert.DoesNotContain("9917", text);
                }
            }
        }
    }

    // --- who may call --------------------------------------------------------------------------------------

    [Fact]
    public async Task It_needs_the_admin_token_and_refuses_an_SDK_key()
    {
        await Seed();
        var body = new { context = new { key = "u1" } };

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await server.Send(HttpMethod.Post, Diagnose, null, body));
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await server.Send(HttpMethod.Post, Diagnose, "Bearer not-a-token", body));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Post, Diagnose, $"Bearer {await Key("server")}", body));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Post, Diagnose, $"Bearer {await Key("client")}", body));
        await ApiServer.Expect(HttpStatusCode.OK, await Admin(body));
    }

    [Fact]
    public async Task It_has_no_CORS()
    {
        await Seed();
        using var client = server.Server.CreateClient();
        using var preflight = new HttpRequestMessage(HttpMethod.Options, Diagnose);
        preflight.Headers.TryAddWithoutValidation("Origin", "https://app.example.test");
        preflight.Headers.TryAddWithoutValidation("Access-Control-Request-Method", "POST");
        preflight.Headers.TryAddWithoutValidation("Access-Control-Request-Headers", "authorization,content-type");
        using var post = new HttpRequestMessage(HttpMethod.Post, Diagnose) { Content = JsonContent.Create(new { context = new { key = "u1" } }) };
        post.Headers.TryAddWithoutValidation("Authorization", $"Bearer {ApiServer.AdminToken}");
        post.Headers.TryAddWithoutValidation("Origin", "https://app.example.test");

        var answered = await client.SendAsync(preflight);
        var served = await client.SendAsync(post);

        Assert.False(answered.Headers.Contains("Access-Control-Allow-Origin"));
        Assert.False(served.Headers.Contains("Access-Control-Allow-Origin"));
    }

    // --- failures, the same as the SDK route's -------------------------------------------------------------

    [Fact]
    public async Task An_unknown_project_or_environment_is_a_404()
    {
        await Seed();
        var body = new { context = new { key = "u1" } };

        var project = await Admin(body, "/api/v1/projects/nope/environments/prod/evaluate");
        var environment = await Admin(body, "/api/v1/projects/shop/environments/nope/evaluate");

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", project);
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", environment);
        Assert.Equal("No such project.", (await ApiServer.Json(project)).GetProperty("error").GetProperty("message").GetString());
        Assert.Equal("No such environment.", (await ApiServer.Json(environment)).GetProperty("error").GetProperty("message").GetString());
    }

    [Fact]
    public async Task An_unknown_or_archived_flag_is_the_same_404_as_the_SDK_route_gives()
    {
        await Seed();
        var client = await Key("client");

        foreach (var flag in new[] { "nope", "old-flag" })
        {
            var ask = new { flagKey = flag, context = new { key = "u1" } };
            var admin = await Admin(ask);
            var sdk = await Sdk(client, ask);

            await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", admin);
            Assert.Equal(await sdk.Content.ReadAsStringAsync(), await admin.Content.ReadAsStringAsync());
        }
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
    public async Task A_missing_or_invalid_context_is_the_same_400_as_the_SDK_route_gives(string json)
    {
        await Seed();
        var sdk = await Raw(SdkEvaluate, $"Bearer {await Key("client")}", json);

        var admin = await Raw(Diagnose, $"Bearer {ApiServer.AdminToken}", json);

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", admin);
        Assert.Equal(await sdk.Content.ReadAsStringAsync(), await admin.Content.ReadAsStringAsync());
    }

    // --- what it says --------------------------------------------------------------------------------------

    [Fact]
    public async Task It_exposes_no_rules_targets_segments_or_salts()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects/shop/environments/prod/segments", new
        {
            key = "secret-segment-name",
            included = new[] { "included-secret-user" },
            excluded = Array.Empty<string>(),
            rules = Array.Empty<object>(),
        }));
        var secrets = new[]
        {
            "vip-secret-user", "internal-attribute", "hidden-value", "secret-segment-name", "included-secret-user",
            server.Salt("flags", "shop", "dark-mode"), server.Salt("flags", "shop", "beta-ui"),
            server.Salt("segments", "shop", "secret-segment-name"),
            "targets", "rules", "clauses", "contextKeys", "segments", "salt", "included", "excluded", "fallthrough", "prerequisites",
        };

        foreach (var ask in new object[]
        {
            new { context = new { key = "u1" } },
            new { context = new { key = "vip-secret-user" } },
            new { flagKey = "dark-mode", context = new { key = "u1", attributes = new Dictionary<string, object> { ["internal-attribute"] = "hidden-value" } } },
        })
        {
            var response = await Admin(ask);
            await ApiServer.Expect(HttpStatusCode.OK, response);
            var text = await response.Content.ReadAsStringAsync();
            foreach (var secret in secrets)
            {
                Assert.DoesNotContain(secret, text);
            }
        }
    }
}
