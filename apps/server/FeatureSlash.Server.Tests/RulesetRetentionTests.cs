using System.Net;
using System.Net.Http.Json;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Sdk;
using Microsoft.Data.Sqlite;
using Microsoft.Extensions.Configuration;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Logging.Abstractions;

/// <summary>A clock stuck at one moment, for the retention window.</summary>
public sealed class FixedClock(DateTime now) : TimeProvider
{
    public override DateTimeOffset GetUtcNow() => new(now, TimeSpan.Zero);
}

/// <summary>
/// <c>RulesetRetention</c>: which ruleset snapshots a sweep deletes. The sweep is called directly, so these do not
/// observe the hourly timer. The server runs with the setting at 0, so the sweep it starts on its own removes nothing
/// and every row a test counts is one the test's own sweep decided about.
/// </summary>
public sealed class RulesetRetentionTests : IDisposable
{
    private const string Rulesets = "/api/v1/projects/shop/environments/{0}/rulesets/{1}";

    private const string EmptyRuleset = """{"flags":[],"segments":[]}""";

    private readonly DateTime now = DateTime.UtcNow;
    private readonly ApiServer server = ApiServer.With(builder => builder.UseSetting(RulesetRetentionSettings.Setting, "0"));

    public void Dispose() => server.Dispose();

    private RulesetRetention Retention(int days) => new(
        server.Server.Services.GetRequiredService<IServiceScopeFactory>(),
        new FixedClock(now),
        new RulesetRetentionSettings(days),
        NullLogger<RulesetRetention>.Instance);

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("shop", "staging");
        await server.SeedFlag("shop", "checkout");
    }

    /// <summary>Serves the environment's ruleset as an SDK does: the hash it answers with is the current one, and it is kept.</summary>
    private async Task<string> Serve(string env)
    {
        var created = await server.Post($"/api/v1/projects/shop/environments/{env}/keys", new { kind = "server", name = "k" });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        var key = (await ApiServer.Json(created)).GetProperty("key").GetString();
        var response = await server.Send(HttpMethod.Get, "/sdk/v1/ruleset", $"Bearer {key}");
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return response.Headers.ETag!.Tag.Trim('"');
    }

    private static List<string> Sorted(params string[] hashes) => [.. hashes.Order(StringComparer.Ordinal)];

    private static string Hash(char c) => new(c, 64);

    private void Sql(string sql, params (string Name, object Value)[] parameters)
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = sql;
        foreach (var (name, value) in parameters)
        {
            command.Parameters.AddWithValue(name, value);
        }
        command.ExecuteNonQuery();
    }

    /// <summary>A snapshot first served this many days ago. A sweep reads only its hash and time; the body is one the lookup route can answer.</summary>
    private void Insert(string env, string hash, double daysAgo) => Sql(
        "INSERT INTO ruleset_snapshots (environment_id, hash, body, first_served_at) SELECT id, $hash, CAST($body AS BLOB), $at FROM environments WHERE key = $env",
        ("$env", env), ("$hash", hash), ("$at", now.AddDays(-daysAgo)), ("$body", EmptyRuleset));

    private void Age(string env, string hash, double daysAgo) => Sql(
        "UPDATE ruleset_snapshots SET first_served_at = $at WHERE hash = $hash AND environment_id = (SELECT id FROM environments WHERE key = $env)",
        ("$env", env), ("$hash", hash), ("$at", now.AddDays(-daysAgo)));

    private List<string> Kept(string env)
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT hash FROM ruleset_snapshots s JOIN environments e ON e.id = s.environment_id WHERE e.key = $env ORDER BY hash";
        command.Parameters.AddWithValue("$env", env);
        using var reader = command.ExecuteReader();
        var hashes = new List<string>();
        while (reader.Read())
        {
            hashes.Add(reader.GetString(0));
        }
        return hashes;
    }

    private async Task<HttpStatusCode> Lookup(string env, string hash) =>
        (await server.Get(string.Format(Rulesets, env, hash))).StatusCode;

    // --- the rule -----------------------------------------------------------------------------------------

    [Fact]
    public async Task A_snapshot_whose_successor_is_older_than_the_window_is_deleted_and_its_hash_then_answers_404()
    {
        await Seed();
        var current = await Serve("prod");
        Insert("prod", Hash('a'), 200);
        Insert("prod", Hash('b'), 100);
        Assert.Equal(HttpStatusCode.OK, await Lookup("prod", Hash('a')));

        var removed = await Retention(90).Sweep(CancellationToken.None);

        Assert.Equal(1, removed);
        Assert.Equal(HttpStatusCode.NotFound, await Lookup("prod", Hash('a')));
        var error = await ApiServer.Json(await server.Get(string.Format(Rulesets, "prod", Hash('a'))));
        Assert.Contains("no longer keeps it", error.GetProperty("error").GetProperty("message").GetString());
        Assert.Equal(Sorted(Hash('b'), current), Kept("prod"));
    }

    [Fact]
    public async Task A_snapshot_first_served_long_ago_is_kept_while_its_successor_is_inside_the_window()
    {
        await Seed();
        var current = await Serve("prod");
        Insert("prod", Hash('a'), 200);
        Age("prod", current, 10);

        var removed = await Retention(90).Sweep(CancellationToken.None);

        Assert.Equal(0, removed);
        Assert.Equal(HttpStatusCode.OK, await Lookup("prod", Hash('a')));
    }

    [Fact]
    public async Task The_window_is_measured_from_when_the_successor_was_first_served_to_the_day()
    {
        await Seed();
        var current = await Serve("prod");
        Age("prod", current, 0);
        Insert("prod", Hash('a'), 200);
        Insert("prod", Hash('b'), 91);
        Insert("prod", Hash('c'), 89);

        // a's successor b is 91 days old: out. b's successor c is 89 days old: in.
        Assert.Equal(1, await Retention(90).Sweep(CancellationToken.None));
        Assert.Equal(Sorted(Hash('b'), Hash('c'), current), Kept("prod"));

        // a shorter window reaches further
        Assert.Equal(1, await Retention(30).Sweep(CancellationToken.None));
        Assert.Equal(Sorted(Hash('c'), current), Kept("prod"));
    }

    [Fact]
    public async Task The_newest_snapshot_is_kept_however_old_it_is()
    {
        await Seed();
        var before = await Serve("prod");
        Age("prod", before, 500);
        await server.SeedFlag("shop", "extra"); // the ruleset changed since: `before` is no longer current
        Insert("prod", Hash('n'), 400); // newest, and not the current ruleset

        var removed = await Retention(90).Sweep(CancellationToken.None);

        Assert.Equal(1, removed); // `before`: its successor is 400 days old and it is not current
        Assert.Equal([Hash('n')], Kept("prod"));
        Assert.Equal(HttpStatusCode.OK, await Lookup("prod", Hash('n')));
    }

    [Fact]
    public async Task An_old_snapshot_whose_hash_is_the_current_ruleset_again_is_kept()
    {
        await Seed();
        var current = await Serve("prod");
        Age("prod", current, 300); // a rollback made this old body current; first_served_at did not move
        Insert("prod", Hash('m'), 200);
        Insert("prod", Hash('n'), 100);

        var removed = await Retention(90).Sweep(CancellationToken.None);

        Assert.Equal(1, removed);
        Assert.Equal(HttpStatusCode.OK, await Lookup("prod", current));
        Assert.Equal(HttpStatusCode.NotFound, await Lookup("prod", Hash('m')));
        Assert.Equal(HttpStatusCode.OK, await Lookup("prod", Hash('n')));
    }

    [Fact]
    public async Task Rows_first_served_at_the_same_moment_are_ordered_by_hash()
    {
        await Seed();
        var current = await Serve("prod");
        Age("prod", current, 0);
        Insert("prod", Hash('b'), 200);
        Insert("prod", Hash('a'), 200);

        // a is first, so its successor b is old enough; b's successor is the current row, which is not
        Assert.Equal(1, await Retention(90).Sweep(CancellationToken.None));
        Assert.Equal(Sorted(Hash('b'), current), Kept("prod"));
    }

    [Fact]
    public async Task One_environments_rows_have_no_effect_on_anothers()
    {
        await Seed();
        var prod = await Serve("prod");
        var staging = await Serve("staging");
        Age("staging", staging, 10);
        // The same hash in both: prod's copy has an old successor, staging's has a recent one.
        Insert("prod", Hash('x'), 200);
        Insert("prod", Hash('y'), 100);
        Insert("staging", Hash('x'), 200);

        var removed = await Retention(90).Sweep(CancellationToken.None);

        // prod's x goes (successor y is 100 days old); staging's x stays (successor is staging's current row, 10 days old).
        Assert.Equal(1, removed);
        Assert.Equal(Sorted(Hash('y'), prod), Kept("prod"));
        Assert.Equal(Sorted(Hash('x'), staging), Kept("staging"));
    }

    // --- the setting --------------------------------------------------------------------------------------

    [Fact]
    public async Task Zero_deletes_nothing()
    {
        await Seed();
        await Serve("prod");
        Insert("prod", Hash('a'), 200);
        Insert("prod", Hash('b'), 100);
        var before = Kept("prod");

        Assert.Equal(0, await Retention(0).Sweep(CancellationToken.None));

        Assert.Equal(before, Kept("prod"));
    }

    [Fact]
    public async Task A_window_longer_than_the_calendar_deletes_nothing_and_does_not_fail()
    {
        await Seed();
        await Serve("prod");
        Insert("prod", Hash('a'), 200);

        Assert.Equal(0, await Retention(int.MaxValue).Sweep(CancellationToken.None));
        Assert.Equal(2, Kept("prod").Count);
    }

    [Fact]
    public void Unset_is_90_days()
    {
        Assert.Equal(90, RulesetRetentionSettings.From(new ConfigurationBuilder().Build()).Days);
    }

    [Fact]
    public void A_whole_number_of_days_is_taken_as_it_is()
    {
        var configuration = new ConfigurationBuilder().AddInMemoryCollection(new Dictionary<string, string?> { [RulesetRetentionSettings.Setting] = "365" }).Build();

        Assert.Equal(365, RulesetRetentionSettings.From(configuration).Days);
    }

    [Theory]
    [InlineData("-1")]
    [InlineData("abc")]
    [InlineData("")]
    public void Any_other_value_stops_startup_naming_the_variable_before_the_database_is_touched(string value)
    {
        using var database = new TempDatabase();
        using var bad = new ServerOn(database.Path, builder => builder.UseSetting(RulesetRetentionSettings.Setting, value));

        var error = Assert.Throws<InvalidOperationException>(bad.Start);

        Assert.Contains("FEATURESLASH_RULESET_RETENTION_DAYS", error.Message);
        Assert.False(File.Exists(database.Path));
    }

    // --- what a sweep does not touch ----------------------------------------------------------------------

    [Fact]
    public async Task A_sweep_that_deleted_rows_leaves_the_audit_trail_unchanged()
    {
        await Seed();
        await Serve("prod");
        Insert("prod", Hash('a'), 200);
        Insert("prod", Hash('b'), 100);
        var audit = server.AuditRows();

        Assert.Equal(1, await Retention(90).Sweep(CancellationToken.None));

        Assert.Equal(audit.Count, server.AuditRows().Count);
        Assert.Equal(audit.Select(a => a.Id), server.AuditRows().Select(a => a.Id));
    }

    [Fact]
    public async Task A_failed_delete_is_not_an_error_and_leaves_the_rows()
    {
        await Seed();
        await Serve("prod");
        Insert("prod", Hash('a'), 200);
        Insert("prod", Hash('b'), 100);
        Sql("CREATE TRIGGER refuse BEFORE DELETE ON ruleset_snapshots BEGIN SELECT RAISE(ABORT, 'refused'); END");
        var before = Kept("prod");

        Assert.Equal(0, await Retention(90).Sweep(CancellationToken.None));

        Assert.Equal(before, Kept("prod"));
        Assert.Equal(HttpStatusCode.OK, await Lookup("prod", Hash('a')));
    }

    // --- the service --------------------------------------------------------------------------------------

    [Fact]
    public async Task A_server_sweeps_once_as_it_starts()
    {
        using var database = new TempDatabase();
        // First run, with retention off: the schema and an environment, then rows written the way the file would hold them.
        using (var first = new ServerOn(database.Path, builder =>
        {
            builder.UseSetting(ServerAuth.AdminTokenSetting, ApiServer.AdminToken);
            builder.UseSetting(RulesetRetentionSettings.Setting, "0");
        }))
        {
            using var client = first.CreateClient();
            client.DefaultRequestHeaders.Authorization = new("Bearer", ApiServer.AdminToken);
            Assert.Equal(HttpStatusCode.Created, (await client.PostAsJsonAsync("/api/v1/projects", new { key = "shop", name = "shop" })).StatusCode);
            Assert.Equal(HttpStatusCode.Created, (await client.PostAsJsonAsync("/api/v1/projects/shop/environments", new { key = "prod", name = "prod" })).StatusCode);
        }
        using (var connection = database.Open())
        {
            using var command = connection.CreateCommand();
            command.CommandText = """
                INSERT INTO ruleset_snapshots (environment_id, hash, body, first_served_at)
                VALUES (1, $old, CAST('{"flags":[],"segments":[]}' AS BLOB), $oldAt), (1, $mid, CAST('{"flags":[],"segments":[]}' AS BLOB), $midAt)
                """;
            command.Parameters.AddWithValue("$old", Hash('a'));
            command.Parameters.AddWithValue("$oldAt", DateTime.UtcNow.AddDays(-300));
            command.Parameters.AddWithValue("$mid", Hash('b'));
            command.Parameters.AddWithValue("$midAt", DateTime.UtcNow.AddDays(-200));
            command.ExecuteNonQuery();
        }

        // Second run: the default window of 90 days, and nothing calls the sweep.
        using var restarted = new ServerOn(database.Path, builder => builder.UseSetting(ServerAuth.AdminTokenSetting, ApiServer.AdminToken));
        restarted.Start();

        var deadline = DateTime.UtcNow.AddSeconds(15);
        List<string> kept;
        do
        {
            await Task.Delay(50);
            kept = Hashes(database);
        }
        while (kept.Contains(Hash('a')) && DateTime.UtcNow < deadline);

        Assert.Equal([Hash('b')], kept);
    }

    private static List<string> Hashes(TempDatabase database)
    {
        using var connection = database.Open();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT hash FROM ruleset_snapshots ORDER BY hash";
        using var reader = command.ExecuteReader();
        var hashes = new List<string>();
        while (reader.Read())
        {
            hashes.Add(reader.GetString(0));
        }
        return hashes;
    }
}
