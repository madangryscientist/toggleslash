using System.Net;
using System.Net.Http.Json;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Data;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Mvc.Testing;
using Microsoft.Data.Sqlite;
using Microsoft.EntityFrameworkCore;
using Microsoft.EntityFrameworkCore.Infrastructure;
using Microsoft.EntityFrameworkCore.Migrations;
using Microsoft.Extensions.DependencyInjection;

public sealed class StorageTests : IDisposable
{
    private static readonly DateTime Now = new(2026, 11, 5, 12, 0, 0, DateTimeKind.Utc);

    private readonly TempDatabase database = new();

    public void Dispose() => database.Dispose();

    [Fact]
    public void A_fresh_database_migrates_on_startup()
    {
        using (var server = new ServerOn(database.Path))
        {
            server.Start();
        }

        Assert.Equal(
            ["admin_tokens", "api_keys", "audit_events", "environments", "flag_environment_configs", "flag_usage_daily", "flags", "projects", "ruleset_snapshots", "segments"],
            Tables());
        Assert.Equal(AllMigrations(), AppliedMigrations());
        Assert.Equal("wal", Scalar("PRAGMA journal_mode"));
    }

    [Fact]
    public void A_second_startup_is_a_no_op()
    {
        using (var first = new ServerOn(database.Path))
        {
            first.Start();
        }
        Execute("INSERT INTO projects (key, name, created_at) VALUES ('p', 'P', '2026-11-05 12:00:00')");
        var schema = Schema();
        var applied = AppliedMigrations();

        using (var second = new ServerOn(database.Path))
        {
            second.Start();
        }

        Assert.Equal(schema, Schema());
        Assert.Equal(applied, AppliedMigrations());
        Assert.Equal(1L, Scalar("SELECT count(*) FROM projects"));
    }

    [Fact]
    public void A_database_at_the_previous_migration_upgrades_without_data_loss()
    {
        var migrations = AllMigrations();
        using (var context = Context())
        {
            context.GetInfrastructure().GetRequiredService<IMigrator>().Migrate(migrations[^2]);
        }
        Assert.Equal(migrations[..^1], AppliedMigrations());
        // Written in SQL, not entities: the entities describe the latest schema, this row is older.
        // The tables and columns date from the first migration, and migrations are additive.
        Execute("""
            INSERT INTO projects (id, key, name, created_at) VALUES (1, 'shop', 'Shop', '2026-11-05 12:00:00');
            INSERT INTO environments (id, project_id, key, name, created_at) VALUES (2, 1, 'prod', 'Prod', '2026-11-05 12:00:00');
            INSERT INTO flags (id, project_id, key, name, description, variations, salt, temporary, tags, created_at)
                VALUES (3, 1, 'checkout', 'Checkout', 'd', '[true,false]', 's', 1, '["a"]', '2026-11-05 12:00:00');
            INSERT INTO flag_environment_configs (flag_id, environment_id, "on", targets, prerequisites, rules, fallthrough, version)
                VALUES (3, 2, 1, '[]', '[]', '[{"clauses":[],"variation":0}]', '{"variation":1}', 7);
            INSERT INTO segments (project_id, environment_id, key, salt, included, excluded, rules)
                VALUES (1, 2, 'beta', 's', '["u"]', '[]', '[]');
            """);

        using var server = new ServerOn(database.Path);
        server.Start();

        Assert.Equal(migrations, AppliedMigrations());
        using var scope = server.Services.CreateScope();
        var stored = scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>();
        var flag = stored.Flags.Single();
        Assert.Equal(("checkout", "[true,false]", true), (flag.Key, flag.Variations, flag.Temporary));
        Assert.Equal(DateTimeKind.Utc, flag.CreatedAt.Kind);
        Assert.Equal(Now, flag.CreatedAt);
        var config = stored.FlagEnvironmentConfigs.Single();
        Assert.Equal((true, 7L, "{\"variation\":1}"), (config.On, config.Version, config.Fallthrough));
        var segment = stored.Segments.Single();
        Assert.Equal(("[\"u\"]", 1L), (segment.Included, segment.Version));
        Assert.Equal(("[]", "[]"), (segment.IncludedContexts, segment.ExcludedContexts));
        Assert.Equal("prod", stored.Environments.Single().Key);
    }

    [Fact]
    public async Task A_segment_from_before_versioning_reads_and_saves_through_the_api()
    {
        var migrations = AllMigrations();
        var before = migrations[migrations.FindIndex(m => m.EndsWith("_AddSegmentVersion", StringComparison.Ordinal)) - 1];
        using (var context = Context())
        {
            context.GetInfrastructure().GetRequiredService<IMigrator>().Migrate(before);
        }
        Execute("""
            INSERT INTO projects (id, key, name, created_at) VALUES (1, 'shop', 'Shop', '2026-11-05 12:00:00');
            INSERT INTO environments (id, project_id, key, name, created_at) VALUES (2, 1, 'prod', 'Prod', '2026-11-05 12:00:00');
            INSERT INTO segments (project_id, environment_id, key, salt, included, excluded, rules)
                VALUES (1, 2, 'beta', 'kept-salt', '["u"]', '[]', '[]');
            """);

        using var server = new ServerOn(database.Path, builder => builder.UseSetting(ServerAuth.AdminTokenSetting, ApiServer.AdminToken));
        using var client = server.CreateClient();
        client.DefaultRequestHeaders.Authorization = new System.Net.Http.Headers.AuthenticationHeaderValue("Bearer", ApiServer.AdminToken);
        const string Path = "/api/v1/projects/shop/environments/prod/segments/beta";

        var read = await ApiServer.Json(await client.GetAsync(Path));
        Assert.Equal("""["u"]""", read.GetProperty("included").GetRawText());
        Assert.Equal(1, read.GetProperty("version").GetInt64());

        var body = new { included = new[] { "v" }, excluded = Array.Empty<string>(), rules = Array.Empty<object>(), version = 1 };
        var saved = await client.PutAsJsonAsync(Path, body);
        await ApiServer.Expect(HttpStatusCode.OK, saved);
        Assert.Equal(2, (await ApiServer.Json(saved)).GetProperty("version").GetInt64());

        Assert.Equal("kept-salt", Scalar("SELECT salt FROM segments"));
    }

    [Fact]
    public void A_failed_migration_rolls_every_pending_migration_back()
    {
        // The last migration creates this table, so it fails after the earlier one has run.
        Execute("CREATE TABLE flag_usage_daily (x)");

        using (var server = new ServerOn(database.Path))
        {
            Assert.Throws<SqliteException>(server.Start);
        }

        Assert.DoesNotContain("projects", Tables());
        Assert.Empty(AppliedMigrations());

        // Nothing is left behind that stops the next startup once the cause is gone.
        Execute("DROP TABLE flag_usage_daily");
        using (var server = new ServerOn(database.Path))
        {
            server.Start();
        }
        Assert.Equal(AllMigrations(), AppliedMigrations());
    }

    [Fact]
    public void Flag_keys_are_unique_per_project()
    {
        using var server = new ServerOn(database.Path);
        server.Start();
        var (shop, blog) = TwoProjects(server);

        Save(server, new Flag { ProjectId = shop, Key = "checkout", Name = "A", Variations = "[]", Salt = "s", CreatedAt = Now });
        Save(server, new Flag { ProjectId = blog, Key = "checkout", Name = "B", Variations = "[]", Salt = "s", CreatedAt = Now });

        Assert.Throws<DbUpdateException>(() =>
            Save(server, new Flag { ProjectId = shop, Key = "checkout", Name = "C", Variations = "[]", Salt = "s", CreatedAt = Now }));
    }

    [Fact]
    public void Environment_keys_are_unique_per_project()
    {
        using var server = new ServerOn(database.Path);
        server.Start();
        var (shop, blog) = TwoProjects(server);

        Save(server, new ProjectEnvironment { ProjectId = shop, Key = "prod", Name = "A", CreatedAt = Now });
        Save(server, new ProjectEnvironment { ProjectId = blog, Key = "prod", Name = "B", CreatedAt = Now });

        Assert.Throws<DbUpdateException>(() =>
            Save(server, new ProjectEnvironment { ProjectId = shop, Key = "prod", Name = "C", CreatedAt = Now }));
    }

    [Fact]
    public void A_local_time_is_stored_as_utc()
    {
        using var server = new ServerOn(database.Path);
        server.Start();

        Save(server, new Project { Key = "shop", Name = "Shop", CreatedAt = Now.ToLocalTime() });

        using var scope = server.Services.CreateScope();
        Assert.Equal(Now, scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>().Projects.Single().CreatedAt);
    }

    // The word is the format on disk, and the check stops the read converter turning a bad value into Client.
    [Fact]
    public void An_api_key_kind_is_stored_as_the_word()
    {
        using var server = new ServerOn(database.Path);
        server.Start();
        var (shop, _) = TwoProjects(server);
        var environment = new ProjectEnvironment { ProjectId = shop, Key = "prod", Name = "Prod", CreatedAt = Now };
        Save(server, environment);

        Save(server, new ApiKey { EnvironmentId = environment.Id, Kind = ApiKeyKind.Client, Name = "web", Hash = "ab", Prefix = "fs_", CreatedAt = Now });

        Assert.Equal("client", Scalar("SELECT kind FROM api_keys"));
        Assert.Throws<SqliteException>(() => Execute("UPDATE api_keys SET kind = 'admin'"));
    }

    [Fact]
    public void The_path_comes_from_the_environment_variable()
    {
        Environment.SetEnvironmentVariable(ServerDatabase.PathSetting, database.Path);
        try
        {
            using var server = new WebApplicationFactory<Program>();
            _ = server.Services;
        }
        finally
        {
            Environment.SetEnvironmentVariable(ServerDatabase.PathSetting, null);
        }

        Assert.Contains("projects", Tables());
    }

    private (long, long) TwoProjects(ServerOn server)
    {
        var shop = new Project { Key = "shop", Name = "Shop", CreatedAt = Now };
        var blog = new Project { Key = "blog", Name = "Blog", CreatedAt = Now };
        Save(server, shop);
        Save(server, blog);
        return (shop.Id, blog.Id);
    }

    private static void Save(ServerOn server, object entity)
    {
        using var scope = server.Services.CreateScope();
        var context = scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>();
        context.Add(entity);
        context.SaveChanges();
    }

    private ToggleSlashDbContext Context() => new(
        new DbContextOptionsBuilder<ToggleSlashDbContext>().UseSqlite($"Data Source={database.Path}").Options);

    private List<string> AllMigrations()
    {
        using var context = Context();
        return [.. context.Database.GetMigrations()];
    }

    private List<string> AppliedMigrations() => Strings("SELECT MigrationId FROM __EFMigrationsHistory ORDER BY MigrationId", ifTable: "__EFMigrationsHistory");

    private List<string> Tables() => Strings("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '\\_\\_EF%' ESCAPE '\\' ORDER BY name");

    private List<string> Schema() => Strings("SELECT coalesce(sql, name) FROM sqlite_master ORDER BY name");

    private List<string> Strings(string sql, string? ifTable = null)
    {
        using var connection = database.Open();
        var rows = new List<string>();
        if (ifTable is not null)
        {
            using var exists = connection.CreateCommand();
            exists.CommandText = $"SELECT count(*) FROM sqlite_master WHERE name = '{ifTable}'";
            if ((long)exists.ExecuteScalar()! == 0)
            {
                return rows;
            }
        }
        using var command = connection.CreateCommand();
        command.CommandText = sql;
        using var reader = command.ExecuteReader();
        while (reader.Read())
        {
            rows.Add(reader.GetString(0));
        }
        return rows;
    }

    private object? Scalar(string sql)
    {
        using var connection = database.Open();
        using var command = connection.CreateCommand();
        command.CommandText = sql;
        return command.ExecuteScalar();
    }

    private void Execute(string sql)
    {
        using var connection = database.Open();
        using var command = connection.CreateCommand();
        command.CommandText = sql;
        command.ExecuteNonQuery();
    }
}
