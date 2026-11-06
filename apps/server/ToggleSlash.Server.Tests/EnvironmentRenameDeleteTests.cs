using System.Net;
using System.Text.Json;

/// <summary>
/// <c>PATCH</c> and <c>DELETE</c> on <c>/api/v1/projects/{project}/environments/{env}</c>. The stream ending after a delete
/// is in <c>SdkStreamTests</c>, which owns the heartbeat seam; the token policy is in <c>AdminTokenTests</c>.
/// </summary>
public sealed class EnvironmentRenameDeleteTests : IDisposable
{
    private const string Shop = "/api/v1/projects/shop";
    private const string Prod = "/api/v1/projects/shop/environments/prod";
    private const string Staging = "/api/v1/projects/shop/environments/staging";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("shop", "staging");
        await server.SeedFlag("shop", "checkout");
    }

    private async Task<(long Id, string Key)> Key(string env, string kind = "server")
    {
        var created = await server.Post($"{Shop}/environments/{env}/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        var body = await ApiServer.Json(created);
        return (body.GetProperty("id").GetInt64(), body.GetProperty("key").GetString()!);
    }

    private async Task Revoke(string env, long id) =>
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"{Shop}/environments/{env}/keys/{id}"));

    private static string ETag(HttpResponseMessage response) => response.Headers.ETag!.Tag;

    private async Task<HttpResponseMessage> Ruleset(string key)
    {
        var response = await server.Send(HttpMethod.Get, "/sdk/v1/ruleset", $"Bearer {key}");
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return response;
    }

    private long Count(string table, string env)
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = $"SELECT COUNT(*) FROM {table} t JOIN environments e ON e.id = t.environment_id WHERE e.key = $env";
        command.Parameters.AddWithValue("$env", env);
        return (long)command.ExecuteScalar()!;
    }

    private long EnvironmentId(string env)
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT id FROM environments WHERE key = $key";
        command.Parameters.AddWithValue("$key", env);
        return (long)command.ExecuteScalar()!;
    }

    private static readonly string[] OwnedTables =
        ["flag_environment_configs", "segments", "api_keys", "flag_usage_daily", "ruleset_snapshots"];

    /// <summary>Gives an environment a row in each of the five tables that hang off it, and ends with every key revoked.</summary>
    private async Task Fill(string env)
    {
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post($"{Shop}/environments/{env}/segments", new
        {
            key = "beta", included = new[] { "u1" }, excluded = Array.Empty<string>(), rules = Array.Empty<object>(),
        }));
        var (serverId, serverKey) = await Key(env);
        var (clientId, clientKey) = await Key(env, "client");
        await Ruleset(serverKey);
        var today = DateTime.UtcNow.ToString("yyyy-MM-dd");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Send(HttpMethod.Post, "/sdk/v1/events", $"Bearer {clientKey}", new
        {
            events = new[] { new { flag = "checkout", variationIndex = 0, count = 3, day = today } },
        }));
        await Revoke(env, serverId);
        await Revoke(env, clientId);
    }

    // --- rename ----------------------------------------------------------------------------------------------

    [Fact]
    public async Task Rename_changes_the_name_trimmed_and_nothing_else()
    {
        await Seed();
        var before = await ApiServer.Json(await server.Get(Prod));

        var response = await server.Patch(Prod, new { name = "  Production EU  " });

        await ApiServer.Expect(HttpStatusCode.OK, response);
        var body = await ApiServer.Json(response);
        Assert.Equal("Production EU", body.GetProperty("name").GetString());
        Assert.Equal("prod", body.GetProperty("key").GetString());
        Assert.Equal(before.GetProperty("createdAt").GetString(), body.GetProperty("createdAt").GetString());
        Assert.Equal(body.GetRawText(), (await ApiServer.Json(await server.Get(Prod))).GetRawText());
        Assert.Equal(["prod", "staging"], (await ApiServer.Json(await server.Get($"{Shop}/environments"))).EnumerateArray().Select(e => e.GetProperty("key").GetString()));
    }

    [Fact]
    public async Task A_key_in_the_body_is_ignored_like_any_unknown_field()
    {
        await Seed();

        var response = await server.Patch(Prod, new { key = "live", name = "Live", colour = "red" });

        await ApiServer.Expect(HttpStatusCode.OK, response);
        Assert.Equal("prod", (await ApiServer.Json(response)).GetProperty("key").GetString());
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get(Prod));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Shop}/environments/live"));
    }

    [Fact]
    public async Task Rename_leaves_the_ruleset_etag_alone()
    {
        await Seed();
        var (_, key) = await Key("prod");
        var before = ETag(await Ruleset(key));

        await ApiServer.Expect(HttpStatusCode.OK, await server.Patch(Prod, new { name = "Renamed" }));

        Assert.Equal(before, ETag(await Ruleset(key)));
    }

    [Fact]
    public async Task Rename_records_one_updated_event_with_the_old_and_new_name()
    {
        await Seed();
        var count = server.AuditRows().Count;

        await ApiServer.Expect(HttpStatusCode.OK, await server.Patch(Prod, new { name = "Renamed" }));

        var rows = server.AuditRows();
        Assert.Equal(count + 1, rows.Count);
        var row = rows[^1];
        Assert.Equal(("environment", "prod", "updated", "shop", "prod", null, "admin"), (row.EntityType, row.EntityId, row.Action, row.Project, row.Environment, row.Flag, row.Actor));
        Assert.Equal("prod", JsonDocument.Parse(row.Before!).RootElement.GetProperty("name").GetString());
        Assert.Equal("Renamed", JsonDocument.Parse(row.After!).RootElement.GetProperty("name").GetString());
    }

    [Theory]
    [InlineData(null)]
    [InlineData("")]
    [InlineData("   ")]
    public async Task A_missing_or_blank_name_is_400_and_changes_nothing(string? name)
    {
        await Seed();
        var count = server.AuditRows().Count;

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await server.Patch(Prod, new { name }));

        Assert.Equal("prod", (await ApiServer.Json(await server.Get(Prod))).GetProperty("name").GetString());
        Assert.Equal(count, server.AuditRows().Count);
    }

    [Fact]
    public async Task Renaming_an_unknown_project_or_environment_is_404()
    {
        await Seed();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Patch("/api/v1/projects/nope/environments/prod", new { name = "X" }));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Patch($"{Shop}/environments/nope", new { name = "X" }));
    }

    // --- delete: the guard -------------------------------------------------------------------------------------

    [Fact]
    public async Task Delete_is_refused_with_409_while_any_key_is_live_and_says_how_many()
    {
        await Seed();
        await Key("prod");
        var (second, _) = await Key("prod", "client");
        await Revoke("prod", second);
        await Key("prod", "client");
        var count = server.AuditRows().Count;

        var response = await server.Delete(Prod);

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", response);
        var message = (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString()!;
        Assert.Contains("2 SDK key", message);
        Assert.Contains("Revoke", message);
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get(Prod));
        Assert.Equal(count, server.AuditRows().Count);
        Assert.Equal(1, Count("flag_environment_configs", "prod"));
    }

    [Fact]
    public async Task A_live_key_in_another_environment_does_not_block()
    {
        await Seed();
        await Key("prod");

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete(Staging));
    }

    [Fact]
    public async Task Delete_succeeds_with_only_revoked_keys_and_answers_404_afterwards()
    {
        await Seed();
        var (id, _) = await Key("prod");
        await Revoke("prod", id);

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete(Prod));

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get(Prod));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Delete(Prod));
        Assert.Equal(["staging"], (await ApiServer.Json(await server.Get($"{Shop}/environments"))).EnumerateArray().Select(e => e.GetProperty("key").GetString()));
    }

    [Fact]
    public async Task Deleting_in_an_unknown_project_or_of_an_unknown_environment_is_404()
    {
        await Seed();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Delete("/api/v1/projects/nope/environments/prod"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Delete($"{Shop}/environments/nope"));
    }

    // --- delete: what goes ---------------------------------------------------------------------------------------

    [Fact]
    public async Task Delete_empties_the_five_tables_for_that_environment_and_touches_no_other()
    {
        await Seed();
        await Fill("prod");
        await Fill("staging");
        foreach (var table in OwnedTables)
        {
            Assert.True(Count(table, "prod") > 0, $"{table} has no prod row to delete");
            Assert.True(Count(table, "staging") > 0, $"{table} has no staging row to keep");
        }
        var kept = OwnedTables.ToDictionary(t => t, t => Count(t, "staging"));
        var prodId = EnvironmentId("prod");

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete(Prod));

        foreach (var table in OwnedTables)
        {
            Assert.Equal(kept[table], Count(table, "staging"));
            using var connection = server.OpenDatabase();
            using var command = connection.CreateCommand();
            command.CommandText = $"SELECT COUNT(*) FROM {table} WHERE environment_id = $id";
            command.Parameters.AddWithValue("$id", prodId);
            Assert.True((long)command.ExecuteScalar()! == 0, $"{table} still has rows of the deleted environment");
        }
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get(Staging));
    }

    [Fact]
    public async Task Delete_keeps_the_audit_trail_and_records_one_deleted_event()
    {
        await Seed();
        await Fill("prod");
        var count = server.AuditRows().Count;

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete(Prod));

        var rows = server.AuditRows();
        Assert.Equal(count + 1, rows.Count);
        var row = rows[^1];
        Assert.Equal(("environment", "prod", "deleted", "shop", "prod", null, "admin"), (row.EntityType, row.EntityId, row.Action, row.Project, row.Environment, row.Flag, row.Actor));
        Assert.Equal("prod", JsonDocument.Parse(row.Before!).RootElement.GetProperty("key").GetString());
        Assert.Null(row.After);
        // The earlier events, its own segment and key included, are still there and still read back.
        Assert.Contains(rows, r => r is { EntityType: "segment", Environment: "prod" });
        Assert.Contains(rows, r => r is { EntityType: "api_key", Environment: "prod" });
        var page = await ApiServer.Json(await server.Get($"{Shop}/audit?environment=prod&limit=100"));
        Assert.Equal(rows.Count(r => r.Environment == "prod"), page.GetProperty("events").GetArrayLength());
    }

    [Fact]
    public async Task The_last_environment_of_a_project_can_be_deleted_and_a_new_one_gets_a_config_for_every_flag()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedFlag("shop", "checkout");

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete(Prod));
        Assert.Equal(0, (await ApiServer.Json(await server.Get($"{Shop}/environments"))).GetArrayLength());
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get($"{Shop}/flags/checkout"));
        Assert.Empty(server.Configs("checkout"));

        await server.SeedEnvironment("shop", "prod");
        Assert.Equal([("prod", false)], server.Configs("checkout").Select(c => (c.Environment, c.On)));
    }

    [Fact]
    public async Task An_environment_created_again_under_the_same_key_is_a_new_one()
    {
        await Seed();
        await Fill("prod");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Shop}/flags/checkout/environments/prod/toggle", new { on = true }));
        var oldId = EnvironmentId("prod");
        var oldHash = HashesOf("prod");
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete(Prod));

        await server.SeedEnvironment("shop", "prod");

        Assert.NotEqual(oldId, EnvironmentId("prod"));
        Assert.Equal([("prod", false)], server.Configs("checkout").Select(c => (c.Environment, c.On)).Where(c => c.Environment == "prod"));
        foreach (var table in new[] { "segments", "api_keys", "flag_usage_daily", "ruleset_snapshots" })
        {
            Assert.Equal(0, Count(table, "prod"));
        }
        // The lookup by a hash the old environment served: the new one never served it.
        Assert.NotEmpty(oldHash);
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Prod}/rulesets/{oldHash[0]}"));
        // The old audit rows are still returned for the key, with the new environment's creation after them.
        var events = (await ApiServer.Json(await server.Get($"{Shop}/audit?environment=prod&limit=100"))).GetProperty("events").EnumerateArray()
            .Select(e => (e.GetProperty("entityType").GetString(), e.GetProperty("action").GetString()))
            .ToList();
        Assert.Contains(("environment", "deleted"), events);
        Assert.Equal(("environment", "created"), events[0]);
        Assert.Contains(("segment", "created"), events);
    }

    private List<string> HashesOf(string env)
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT s.hash FROM ruleset_snapshots s JOIN environments e ON e.id = s.environment_id WHERE e.key = $env";
        command.Parameters.AddWithValue("$env", env);
        using var reader = command.ExecuteReader();
        var hashes = new List<string>();
        while (reader.Read())
        {
            hashes.Add(reader.GetString(0));
        }
        return hashes;
    }

    [Fact]
    public async Task The_runtime_read_and_the_ruleset_lookup_of_a_deleted_environment_answer_the_environment_404()
    {
        await Seed();
        await Fill("prod");
        var hash = (HashesOf("prod")).First();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get($"{Prod}/rulesets/{hash}"));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get($"{Prod}/runtime"));

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete(Prod));

        var lookup = await server.Get($"{Prod}/rulesets/{hash}");
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", lookup);
        Assert.Equal("No such environment.", (await ApiServer.Json(lookup)).GetProperty("error").GetProperty("message").GetString());
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Prod}/runtime"));
    }
}
