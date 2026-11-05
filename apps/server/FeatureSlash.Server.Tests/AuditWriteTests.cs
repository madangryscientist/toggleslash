using System.Net;
using System.Text.Json;
using Microsoft.AspNetCore.Routing;
using Microsoft.Extensions.DependencyInjection;

/// <summary>Every admin write records exactly one audit event in its own transaction; a failed write records none.</summary>
public sealed class AuditWriteTests : IDisposable
{
    private const string Shop = "/api/v1/projects/shop";
    private const string Flag = "/api/v1/projects/shop/flags/checkout";
    private const string Config = "/api/v1/projects/shop/flags/checkout/environments/prod";
    private const string Segments = "/api/v1/projects/shop/environments/prod/segments";
    private const string Keys = "/api/v1/projects/shop/environments/prod/keys";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedFlag("shop", "checkout");
    }

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

    private sealed record Step(string Route, Func<Task<HttpResponseMessage>> Send, HttpStatusCode Expected, string EntityType, string Action);

    private IEnumerable<Step> EveryWriteRoute()
    {
        long keyId = 0;
        yield return new("POST /api/v1/projects", () => server.Post("/api/v1/projects", new { key = "other", name = "Other" }), HttpStatusCode.Created, "project", "created");
        yield return new("POST /api/v1/projects/{project}/environments", () => server.Post($"{Shop}/environments", new { key = "staging", name = "Staging" }), HttpStatusCode.Created, "environment", "created");
        yield return new("POST /api/v1/projects/{project}/flags", () => server.Post($"{Shop}/flags", new { key = "banner", name = "Banner", variations = new[] { true, false } }), HttpStatusCode.Created, "flag", "created");
        yield return new("PATCH /api/v1/projects/{project}/flags/{flag}", () => server.Patch(Flag, new { name = "Checkout v2" }), HttpStatusCode.OK, "flag", "updated");
        yield return new("POST /api/v1/projects/{project}/flags/{flag}/archive", () => server.Post($"{Flag}/archive"), HttpStatusCode.OK, "flag", "archived");
        yield return new("POST /api/v1/projects/{project}/flags/{flag}/restore", () => server.Post($"{Flag}/restore"), HttpStatusCode.OK, "flag", "restored");
        yield return new("PUT /api/v1/projects/{project}/flags/{flag}/environments/{env}", () => server.Put(Config, ConfigBody(1)), HttpStatusCode.OK, "flag_config", "updated");
        yield return new("POST /api/v1/projects/{project}/flags/{flag}/environments/{env}/toggle", () => server.Post($"{Config}/toggle", new { on = false }), HttpStatusCode.OK, "flag_config", "toggled");
        yield return new("POST /api/v1/projects/{project}/environments/{env}/segments", () => server.Post(Segments, new { key = "beta", included = new[] { "u1" }, excluded = Array.Empty<string>(), rules = Array.Empty<object>() }), HttpStatusCode.Created, "segment", "created");
        yield return new("PUT /api/v1/projects/{project}/environments/{env}/segments/{segment}", () => server.Put($"{Segments}/beta", new { included = new[] { "u2" }, excluded = Array.Empty<string>(), rules = Array.Empty<object>(), version = 1 }), HttpStatusCode.OK, "segment", "updated");
        yield return new("DELETE /api/v1/projects/{project}/environments/{env}/segments/{segment}", () => server.Delete($"{Segments}/beta"), HttpStatusCode.NoContent, "segment", "deleted");
        yield return new("POST /api/v1/projects/{project}/environments/{env}/keys", async () =>
        {
            var response = await server.Post(Keys, new { kind = "server", name = "backend" });
            keyId = (await ApiServer.Json(response)).GetProperty("id").GetInt64();
            return response;
        }, HttpStatusCode.Created, "api_key", "created");
        yield return new("DELETE /api/v1/projects/{project}/environments/{env}/keys/{id:long}", () => server.Delete($"{Keys}/{keyId}"), HttpStatusCode.NoContent, "api_key", "revoked");
    }

    [Fact]
    public async Task Every_write_route_records_exactly_one_event()
    {
        await Seed();

        foreach (var step in EveryWriteRoute())
        {
            var before = server.AuditRows().Count;
            await ApiServer.Expect(step.Expected, await step.Send());

            var rows = server.AuditRows();
            Assert.True(rows.Count == before + 1, $"{step.Route} recorded {rows.Count - before} events");
            Assert.Equal((step.EntityType, step.Action, "admin"), (rows[^1].EntityType, rows[^1].Action, rows[^1].Actor));
        }
    }

    [Fact]
    public void The_steps_cover_every_write_route_the_server_has()
    {
        var served = server.Server.Services.GetRequiredService<EndpointDataSource>().Endpoints
            .OfType<RouteEndpoint>()
            .Where(e => e.RoutePattern.RawText!.StartsWith("/api/", StringComparison.Ordinal))
            .SelectMany(e => (e.Metadata.GetMetadata<HttpMethodMetadata>()?.HttpMethods ?? [])
                .Where(method => method != "GET")
                .Select(method => $"{method} {e.RoutePattern.RawText}"))
            .Select(route => route.TrimEnd('/'))
            .Order()
            .ToList();

        // A new write route fails here until it has a step above, and with it a proof that it records an event.
        Assert.Equal(served, EveryWriteRoute().Select(s => s.Route.TrimEnd('/')).Order().ToList());
    }

    [Fact]
    public async Task The_event_names_who_what_and_where_with_before_and_after()
    {
        await Seed();
        var created = server.AuditRows().Single(r => r is { EntityType: "flag", Action: "created" });
        Assert.Equal(("checkout", "shop", null, "checkout", null), (created.EntityId, created.Project, created.Environment, created.Flag, created.Before));
        Assert.Equal("checkout", JsonDocument.Parse(created.After!).RootElement.GetProperty("key").GetString());

        await ApiServer.Expect(HttpStatusCode.OK, await server.Patch(Flag, new { name = "Checkout v2", tags = new[] { "payments" } }));
        var updated = server.AuditRows()[^1];
        Assert.Equal("checkout", JsonDocument.Parse(updated.Before!).RootElement.GetProperty("name").GetString());
        var after = JsonDocument.Parse(updated.After!).RootElement;
        Assert.Equal("Checkout v2", after.GetProperty("name").GetString());
        Assert.Equal("payments", after.GetProperty("tags")[0].GetString());

        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Config}/toggle", new { on = true }));
        var toggled = server.AuditRows()[^1];
        Assert.Equal(("flag_config", "checkout/prod", "toggled", "prod", "checkout"), (toggled.EntityType, toggled.EntityId, toggled.Action, toggled.Environment, toggled.Flag));
        Assert.False(JsonDocument.Parse(toggled.Before!).RootElement.GetProperty("on").GetBoolean());
        Assert.True(JsonDocument.Parse(toggled.After!).RootElement.GetProperty("on").GetBoolean());
        Assert.Equal(2, JsonDocument.Parse(toggled.After!).RootElement.GetProperty("version").GetInt64());
    }

    [Fact]
    public async Task A_write_that_changes_nothing_still_records_one_event_with_equal_before_and_after()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flag}/archive"));
        var count = server.AuditRows().Count;

        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flag}/archive"));

        var rows = server.AuditRows();
        Assert.Equal(count + 1, rows.Count);
        Assert.Equal(rows[^1].Before, rows[^1].After);
    }

    public static TheoryData<string> RejectedWrites => new()
    {
        "bad project key",
        "project exists",
        "environment in a missing project",
        "flag with one variation",
        "flag key taken",
        "config with a stale version",
        "toggle with a stale version",
        "invalid config",
        "delete a segment a rule uses",
        "revoke a missing key",
        "no token",
        "an SDK key",
    };

    [Theory]
    [MemberData(nameof(RejectedWrites))]
    public async Task A_rejected_write_records_nothing(string scenario)
    {
        await Seed();
        await server.Post(Segments, new { key = "beta", included = Array.Empty<string>(), excluded = Array.Empty<string>(), rules = Array.Empty<object>() });
        var sdkKey = (await ApiServer.Json(await server.Post(Keys, new { kind = "server", name = "backend" }))).GetProperty("key").GetString()!;
        var rule = new[] { new { clauses = new[] { new { op = "segmentMatch", values = new[] { "beta" } } }, variation = 0 } };
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put(Config, new
        {
            on = true, offVariation = 1, targets = Array.Empty<object>(), prerequisites = Array.Empty<object>(), rules = rule, fallthrough = new { variation = 0 }, version = 1,
        }));
        var before = server.AuditRows().Count;

        var (expected, response) = scenario switch
        {
            "bad project key" => (HttpStatusCode.BadRequest, await server.Post("/api/v1/projects", new { key = "Not A Key", name = "x" })),
            "project exists" => (HttpStatusCode.Conflict, await server.Post("/api/v1/projects", new { key = "shop", name = "Shop" })),
            "environment in a missing project" => (HttpStatusCode.NotFound, await server.Post("/api/v1/projects/nope/environments", new { key = "dev", name = "Dev" })),
            "flag with one variation" => (HttpStatusCode.BadRequest, await server.Post($"{Shop}/flags", new { key = "solo", name = "Solo", variations = new[] { true } })),
            "flag key taken" => (HttpStatusCode.Conflict, await server.Post($"{Shop}/flags", new { key = "checkout", name = "Again", variations = new[] { true, false } })),
            "config with a stale version" => (HttpStatusCode.Conflict, await server.Put(Config, ConfigBody(1))),
            "toggle with a stale version" => (HttpStatusCode.Conflict, await server.Post($"{Config}/toggle", new { on = false, version = 1 })),
            "invalid config" => (HttpStatusCode.BadRequest, await server.Put(Config, new { on = true, offVariation = 9, targets = Array.Empty<object>(), prerequisites = Array.Empty<object>(), rules = Array.Empty<object>(), fallthrough = new { variation = 0 }, version = 2 })),
            "delete a segment a rule uses" => (HttpStatusCode.Conflict, await server.Delete($"{Segments}/beta")),
            "revoke a missing key" => (HttpStatusCode.NotFound, await server.Delete($"{Keys}/9999")),
            "no token" => (HttpStatusCode.Unauthorized, await server.Send(HttpMethod.Post, "/api/v1/projects", null, new { key = "nope", name = "x" })),
            "an SDK key" => (HttpStatusCode.Forbidden, await server.Send(HttpMethod.Post, "/api/v1/projects", $"Bearer {sdkKey}", new { key = "nope", name = "x" })),
            _ => throw new ArgumentException(scenario),
        };

        await ApiServer.Expect(expected, response);
        Assert.Equal(before, server.AuditRows().Count);
    }

    [Fact]
    public async Task An_event_that_cannot_be_written_takes_its_write_with_it()
    {
        await Seed();
        await server.Post(Segments, new { key = "beta", included = Array.Empty<string>(), excluded = Array.Empty<string>(), rules = Array.Empty<object>() });
        using (var connection = server.OpenDatabase())
        {
            using var command = connection.CreateCommand();
            command.CommandText = "CREATE TRIGGER audit_down BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT, 'audit down'); END";
            command.ExecuteNonQuery();
        }
        var rows = server.AuditRows().Count;

        // One route per way a write is made: a tracked save, a save after a first save, and the two bulk statements.
        await ApiServer.Expect(HttpStatusCode.InternalServerError, await server.Post("/api/v1/projects", new { key = "other", name = "Other" }));
        await ApiServer.Expect(HttpStatusCode.InternalServerError, await server.Post($"{Shop}/environments", new { key = "staging", name = "Staging" }));
        await ApiServer.Expect(HttpStatusCode.InternalServerError, await server.Post($"{Flag}/archive"));
        await ApiServer.Expect(HttpStatusCode.InternalServerError, await server.Post($"{Config}/toggle", new { on = true }));
        await ApiServer.Expect(HttpStatusCode.InternalServerError, await server.Put(Config, ConfigBody(1)));
        await ApiServer.Expect(HttpStatusCode.InternalServerError, await server.Delete($"{Segments}/beta"));
        await ApiServer.Expect(HttpStatusCode.InternalServerError, await server.Post(Keys, new { kind = "client", name = "web" }));

        Assert.Equal(rows, server.AuditRows().Count);
        await ApiServer.Expect(HttpStatusCode.NotFound, await server.Get("/api/v1/projects/other"));
        await ApiServer.Expect(HttpStatusCode.NotFound, await server.Get($"{Shop}/environments/staging"));
        Assert.Null((await ApiServer.Json(await server.Get(Flag))).GetProperty("archivedAt").GetString());
        var config = await ApiServer.Json(await server.Get(Config));
        Assert.Equal((false, 1), (config.GetProperty("on").GetBoolean(), (int)config.GetProperty("version").GetInt64()));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get($"{Segments}/beta"));
        Assert.Empty((await ApiServer.Json(await server.Get(Keys))).EnumerateArray());
    }

    [Fact]
    public async Task No_plaintext_key_hash_or_salt_appears_in_any_audit_row()
    {
        await Seed();
        await server.SeedEnvironment("shop", "staging");
        await server.Post(Segments, new { key = "beta", included = new[] { "u1" }, excluded = Array.Empty<string>(), rules = Array.Empty<object>() });
        var secrets = new List<string> { server.Salt("flags", "shop", "checkout"), server.Salt("segments", "shop", "beta") };
        foreach (var kind in new[] { "server", "client" })
        {
            var created = await ApiServer.Json(await server.Post(Keys, new { kind, name = $"{kind} key" }));
            secrets.Add(created.GetProperty("key").GetString()!);
            await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"{Keys}/{created.GetProperty("id").GetInt64()}"));
        }
        await server.Patch(Flag, new { description = "touched" });
        await server.Post($"{Flag}/archive");
        await server.Put($"{Segments}/beta", new { included = Array.Empty<string>(), excluded = Array.Empty<string>(), rules = Array.Empty<object>(), version = 1 });
        await server.Delete($"{Segments}/beta");

        using (var connection = server.OpenDatabase())
        {
            using var command = connection.CreateCommand();
            command.CommandText = "SELECT hash FROM api_keys";
            using var reader = command.ExecuteReader();
            while (reader.Read())
            {
                secrets.Add(reader.GetString(0));
            }
        }

        // Two salts, two plain keys, two key hashes: nothing short enough to match by accident.
        Assert.Equal(6, secrets.Count);
        Assert.All(secrets, secret => Assert.True(secret.Length >= 16, secret));
        var rows = server.AuditRows();
        Assert.Contains(rows, r => r.EntityType == "api_key");
        foreach (var row in rows)
        {
            foreach (var secret in secrets)
            {
                Assert.DoesNotContain(secret, row.Everything);
            }
        }
    }
}
