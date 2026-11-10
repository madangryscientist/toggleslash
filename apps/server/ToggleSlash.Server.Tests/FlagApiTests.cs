using System.Net;
using System.Text.Json;

public sealed class FlagApiTests : IDisposable
{
    private const string Flags = "/api/v1/projects/shop/flags";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private async Task<string> ProjectWithEnvironments(params string[] environments)
    {
        await server.SeedProject();
        foreach (var environment in environments)
        {
            await server.SeedEnvironment("shop", environment);
        }
        return "shop";
    }

    private static string[] Keys(JsonElement list) => list.EnumerateArray().Select(f => f.GetProperty("key").GetString()!).ToArray();

    [Fact]
    public async Task Create_returns_the_flag_and_it_reads_back()
    {
        await ProjectWithEnvironments("prod");

        var created = await server.Post(Flags, new
        {
            key = "dark-mode",
            name = " Dark mode ",
            description = "The dark theme.",
            variations = new[] { true, false },
            temporary = true,
            owner = " growth ",
            tags = new[] { "ui", "ui", "theme" },
        });

        await ApiServer.Expect(HttpStatusCode.Created, created);
        Assert.Equal($"{Flags}/dark-mode", created.Headers.Location!.OriginalString);
        var body = await ApiServer.Json(created);
        Assert.Equal("dark-mode", body.GetProperty("key").GetString());
        Assert.Equal("Dark mode", body.GetProperty("name").GetString());
        Assert.Equal("The dark theme.", body.GetProperty("description").GetString());
        Assert.Equal("[true,false]", body.GetProperty("variations").GetRawText());
        Assert.True(body.GetProperty("temporary").GetBoolean());
        Assert.Equal("growth", body.GetProperty("owner").GetString());
        Assert.Equal("[\"ui\",\"theme\"]", body.GetProperty("tags").GetRawText());
        Assert.Equal(JsonValueKind.Null, body.GetProperty("archivedAt").ValueKind);
        Assert.False(body.TryGetProperty("salt", out _));

        var read = await ApiServer.Json(await server.Get($"{Flags}/dark-mode"));
        Assert.Equal(body.GetRawText(), read.GetRawText());
    }

    [Fact]
    public async Task Defaults_are_empty_description_no_owner_no_tags_and_not_temporary()
    {
        await ProjectWithEnvironments();

        var body = await ApiServer.Json(await server.Post(Flags, new { key = "f", name = "F", variations = new[] { "a", "b", "c" } }));

        Assert.Equal("", body.GetProperty("description").GetString());
        Assert.Equal(JsonValueKind.Null, body.GetProperty("owner").ValueKind);
        Assert.Equal(0, body.GetProperty("tags").GetArrayLength());
        Assert.False(body.GetProperty("temporary").GetBoolean());
        Assert.Equal("""["a","b","c"]""", body.GetProperty("variations").GetRawText());
    }

    [Fact]
    public async Task A_new_flag_gets_an_off_config_in_every_environment_of_its_project_only()
    {
        await ProjectWithEnvironments("prod", "staging");
        await server.SeedProject("blog");
        await server.SeedEnvironment("blog", "prod");

        await server.SeedFlag("shop", "dark-mode");

        Assert.Equal(
            [("prod", false, 1L, """{"variation":0}"""), ("staging", false, 1L, """{"variation":0}""")],
            server.Configs("dark-mode"));
    }

    [Fact]
    public async Task The_off_and_fallthrough_variations_can_be_chosen()
    {
        await ProjectWithEnvironments("prod");

        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Flags, new
        {
            key = "plan",
            name = "Plan",
            variations = new[] { "free", "pro", "team" },
            offVariation = 2,
            fallthroughVariation = 1,
        }));

        Assert.Equal([("prod", false, 2L, """{"variation":1}""")], server.Configs("plan"));
    }

    [Fact]
    public async Task Variations_may_be_numbers_strings_arrays_or_objects()
    {
        await ProjectWithEnvironments();

        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Flags, new { key = "numbers", name = "n", variations = new object[] { 0, 1.5, 100 } }));
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Flags, new { key = "objects", name = "o", variations = new object[] { new { size = 1 }, new { size = 2 } } }));
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Flags, new { key = "arrays", name = "a", variations = new object[] { new[] { 1 }, Array.Empty<int>() } }));

        var objects = await ApiServer.Json(await server.Get($"{Flags}/objects"));
        Assert.Equal("""[{"size":1},{"size":2}]""", objects.GetProperty("variations").GetRawText());
    }

    public static TheoryData<string, object?> InvalidCreates => new()
    {
        { "no key", new { name = "F", variations = new[] { true, false } } },
        { "bad key", new { key = "Dark Mode", name = "F", variations = new[] { true, false } } },
        { "no name", new { key = "f", variations = new[] { true, false } } },
        { "no variations", new { key = "f", name = "F" } },
        { "one variation", new { key = "f", name = "F", variations = new[] { true } } },
        { "no variations at all", new { key = "f", name = "F", variations = Array.Empty<bool>() } },
        { "mixed types", new { key = "f", name = "F", variations = new object[] { true, "no" } } },
        { "number and string", new { key = "f", name = "F", variations = new object[] { 1, "1" } } },
        { "a null variation", new { key = "f", name = "F", variations = new object?[] { null, true } } },
        { "object and array", new { key = "f", name = "F", variations = new object[] { new { a = 1 }, new[] { 1 } } } },
        { "bad tag", new { key = "f", name = "F", variations = new[] { true, false }, tags = new[] { "Bad Tag" } } },
        { "offVariation past the end", new { key = "f", name = "F", variations = new[] { true, false }, offVariation = 2 } },
        { "negative offVariation", new { key = "f", name = "F", variations = new[] { true, false }, offVariation = -1 } },
        { "fallthroughVariation past the end", new { key = "f", name = "F", variations = new[] { true, false }, fallthroughVariation = 5 } },
        { "an index sent as a string", new { key = "f", name = "F", variations = new[] { true, false }, offVariation = "1" } },
        { "long description", new { key = "f", name = "F", variations = new[] { true, false }, description = new string('x', 2001) } },
    };

    [Theory]
    [MemberData(nameof(InvalidCreates))]
    public async Task Invalid_input_is_400_and_stores_nothing(string why, object? body)
    {
        await ProjectWithEnvironments("prod");

        var response = await server.Post(Flags, body);

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", response);
        Assert.True(Keys(await ApiServer.Json(await server.Get($"{Flags}?archived=all"))).Length == 0, why);
    }

    [Fact]
    public async Task Variations_of_the_wrong_json_type_are_400()
    {
        await ProjectWithEnvironments();

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await server.Post(Flags, new { key = "f", name = "F", variations = "true,false" }));
    }

    [Fact]
    public async Task A_taken_key_is_409_even_when_the_flag_is_archived()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "dark-mode");

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", await server.Post(Flags, new { key = "dark-mode", name = "Again", variations = new[] { true, false } }));

        await server.Post($"{Flags}/dark-mode/archive");
        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", await server.Post(Flags, new { key = "dark-mode", name = "Again", variations = new[] { true, false } }));
        Assert.Single(server.Configs("dark-mode"));
    }

    [Fact]
    public async Task The_same_key_in_another_project_is_fine()
    {
        await ProjectWithEnvironments();
        await server.SeedProject("blog");
        await server.SeedFlag("shop", "dark-mode");

        await server.SeedFlag("blog", "dark-mode");
    }

    [Fact]
    public async Task An_unknown_project_or_flag_is_404_for_every_flag_route()
    {
        await ProjectWithEnvironments();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Post("/api/v1/projects/nope/flags", new { key = "f", name = "F", variations = new[] { true, false } }));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/nope/flags"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/nope/flags/f"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Flags}/missing"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.AsAdmin(HttpMethod.Patch, $"{Flags}/missing", new { name = "x" }));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Post($"{Flags}/missing/archive"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Post($"{Flags}/missing/restore"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Post("/api/v1/projects/nope/flags/f/archive"));
    }

    [Fact]
    public async Task A_flag_of_another_project_is_not_found()
    {
        await ProjectWithEnvironments();
        await server.SeedProject("blog");
        await server.SeedFlag("blog", "only-in-blog");

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Flags}/only-in-blog"));
    }

    [Fact]
    public async Task List_hides_archived_flags_by_default_and_filters_by_tag()
    {
        await ProjectWithEnvironments();
        await server.Post(Flags, new { key = "a", name = "A", variations = new[] { true, false }, tags = new[] { "ui", "beta" } });
        await server.Post(Flags, new { key = "b", name = "B", variations = new[] { true, false }, tags = new[] { "api" } });
        await server.Post(Flags, new { key = "c", name = "C", variations = new[] { true, false }, tags = new[] { "ui" } });
        await server.Post($"{Flags}/c/archive");

        Assert.Equal(["a", "b"], Keys(await ApiServer.Json(await server.Get(Flags))));
        Assert.Equal(["a", "b"], Keys(await ApiServer.Json(await server.Get($"{Flags}?archived=false"))));
        Assert.Equal(["c"], Keys(await ApiServer.Json(await server.Get($"{Flags}?archived=true"))));
        Assert.Equal(["a", "b", "c"], Keys(await ApiServer.Json(await server.Get($"{Flags}?archived=all"))));
        Assert.Equal(["a"], Keys(await ApiServer.Json(await server.Get($"{Flags}?tag=ui"))));
        Assert.Equal(["a", "c"], Keys(await ApiServer.Json(await server.Get($"{Flags}?tag=ui&archived=all"))));
        Assert.Equal(["c"], Keys(await ApiServer.Json(await server.Get($"{Flags}?tag=ui&archived=true"))));
        Assert.Empty(Keys(await ApiServer.Json(await server.Get($"{Flags}?tag=u"))));
        Assert.Empty(Keys(await ApiServer.Json(await server.Get($"{Flags}?tag=nope"))));
    }

    [Fact]
    public async Task An_unknown_archived_filter_is_400()
    {
        await ProjectWithEnvironments();

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await server.Get($"{Flags}?archived=maybe"));
    }

    [Fact]
    public async Task Update_changes_only_the_fields_sent()
    {
        await ProjectWithEnvironments();
        await server.Post(Flags, new { key = "f", name = "F", description = "d", variations = new[] { true, false }, owner = "me", tags = new[] { "a" } });

        var renamed = await ApiServer.Json(await server.AsAdmin(HttpMethod.Patch, $"{Flags}/f", new { name = " New name ", temporary = true }));

        Assert.Equal("New name", renamed.GetProperty("name").GetString());
        Assert.True(renamed.GetProperty("temporary").GetBoolean());
        Assert.Equal("d", renamed.GetProperty("description").GetString());
        Assert.Equal("me", renamed.GetProperty("owner").GetString());
        Assert.Equal("""["a"]""", renamed.GetProperty("tags").GetRawText());
        Assert.Equal("[true,false]", renamed.GetProperty("variations").GetRawText());

        var rest = await ApiServer.Json(await server.AsAdmin(HttpMethod.Patch, $"{Flags}/f", new { description = "", owner = "", tags = new[] { "x", "y" } }));
        Assert.Equal("", rest.GetProperty("description").GetString());
        Assert.Equal(JsonValueKind.Null, rest.GetProperty("owner").ValueKind);
        Assert.Equal("""["x","y"]""", rest.GetProperty("tags").GetRawText());

        var read = await ApiServer.Json(await server.Get($"{Flags}/f"));
        Assert.Equal(rest.GetRawText(), read.GetRawText());
    }

    [Fact]
    public async Task Update_leaves_variations_and_configs_alone()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "f");

        // Variations are fixed once the flag exists, so a patch that names them changes nothing.
        var response = await ApiServer.Json(await server.AsAdmin(HttpMethod.Patch, $"{Flags}/f", new { name = "G", variations = new[] { 1, 2, 3 } }));

        Assert.Equal("[true,false]", response.GetProperty("variations").GetRawText());
        Assert.Equal([("prod", false, 1L, """{"variation":0}""")], server.Configs("f"));
    }

    [Theory]
    [InlineData("""{"name":""}""")]
    [InlineData("""{"name":"   "}""")]
    [InlineData("""{"tags":["Bad Tag"]}""")]
    public async Task An_invalid_update_is_400_and_changes_nothing(string json)
    {
        await ProjectWithEnvironments();
        await server.SeedFlag("shop", "f");
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Patch, $"{Flags}/f") { Content = new StringContent(json, System.Text.Encoding.UTF8, "application/json") };
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {ApiServer.AdminToken}");

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await client.SendAsync(request));

        var read = await ApiServer.Json(await server.Get($"{Flags}/f"));
        Assert.Equal("f", read.GetProperty("name").GetString());
    }

    [Fact]
    public async Task Archive_and_restore_round_trip_and_keep_the_configs()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "f");

        var archived = await server.Post($"{Flags}/f/archive");
        await ApiServer.Expect(HttpStatusCode.OK, archived);
        var archivedAt = (await ApiServer.Json(archived)).GetProperty("archivedAt").GetDateTime();
        Assert.True(archivedAt > DateTime.UtcNow.AddMinutes(-1));
        Assert.Single(server.Configs("f"));

        var again = await ApiServer.Json(await server.Post($"{Flags}/f/archive"));
        Assert.Equal(archivedAt, again.GetProperty("archivedAt").GetDateTime());
        var read = await ApiServer.Json(await server.Get($"{Flags}/f"));
        Assert.Equal(archivedAt, read.GetProperty("archivedAt").GetDateTime());

        var restored = await server.Post($"{Flags}/f/restore");
        await ApiServer.Expect(HttpStatusCode.OK, restored);
        Assert.Equal(JsonValueKind.Null, (await ApiServer.Json(restored)).GetProperty("archivedAt").ValueKind);
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flags}/f/restore"));
        Assert.Equal(["f"], Keys(await ApiServer.Json(await server.Get(Flags))));
    }

    [Fact]
    public async Task An_archived_flag_can_still_be_read_and_updated()
    {
        await ProjectWithEnvironments();
        await server.SeedFlag("shop", "f");
        await server.Post($"{Flags}/f/archive");

        var updated = await ApiServer.Json(await server.AsAdmin(HttpMethod.Patch, $"{Flags}/f", new { name = "Renamed" }));

        Assert.Equal("Renamed", updated.GetProperty("name").GetString());
        Assert.NotEqual(JsonValueKind.Null, updated.GetProperty("archivedAt").ValueKind);
    }

    // --- lifecycle ---------------------------------------------------------------------------------------------------

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

    /// <summary>Straight into the file: a flag created <paramref name="createdDaysAgo"/> days ago, with a usage row <paramref name="usedDaysAgo"/> days ago if given.</summary>
    private void Backdate(string flag, int createdDaysAgo, int? usedDaysAgo = null)
    {
        Sql("UPDATE flags SET created_at = $at WHERE key = $key", ("$at", DateTime.UtcNow.AddDays(-createdDaysAgo).ToString("yyyy-MM-dd HH:mm:ss.fffffff")), ("$key", flag));
        if (usedDaysAgo is { } days)
        {
            Sql(
                "INSERT INTO flag_usage_daily (environment_id, flag_id, variation, day, count) SELECT e.id, f.id, 0, $day, 3 FROM flags f, environments e WHERE f.key = $key AND e.key = 'prod'",
                ("$day", DateTime.UtcNow.AddDays(-days).ToString("yyyy-MM-dd")),
                ("$key", flag));
        }
    }

    private async Task<string> Lifecycle(string flag) =>
        (await ApiServer.Json(await server.Get($"{Flags}/{flag}"))).GetProperty("lifecycle").GetString()!;

    [Fact]
    public async Task A_new_flag_is_new()
    {
        await ProjectWithEnvironments("prod");

        var created = await server.Post(Flags, new { key = "f", name = "f", variations = new[] { true, false } });

        Assert.Equal("new", (await ApiServer.Json(created)).GetProperty("lifecycle").GetString());
        Assert.Equal("new", await Lifecycle("f"));
    }

    [Fact]
    public async Task A_flag_with_usage_today_is_active_even_when_it_is_old()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "f");
        Backdate("f", createdDaysAgo: 30, usedDaysAgo: 0);

        Assert.Equal("active", await Lifecycle("f"));
    }

    [Fact]
    public async Task Usage_six_days_ago_still_counts_and_seven_days_ago_does_not()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "six");
        await server.SeedFlag("shop", "seven");
        Backdate("six", createdDaysAgo: 30, usedDaysAgo: 6);
        Backdate("seven", createdDaysAgo: 30, usedDaysAgo: 7);

        Assert.Equal("active", await Lifecycle("six"));
        Assert.Equal("inactive", await Lifecycle("seven"));
    }

    [Fact]
    public async Task An_old_flag_with_no_usage_is_inactive()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "f");
        Backdate("f", createdDaysAgo: 8);

        Assert.Equal("inactive", await Lifecycle("f"));
    }

    [Fact]
    public async Task An_archived_flag_is_archived_even_with_usage_today()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "f");
        Backdate("f", createdDaysAgo: 0, usedDaysAgo: 0);

        var archived = await server.Post($"{Flags}/f/archive");

        Assert.Equal("archived", (await ApiServer.Json(archived)).GetProperty("lifecycle").GetString());
        Assert.Equal("archived", await Lifecycle("f"));
    }

    [Fact]
    public async Task Every_route_that_returns_a_flag_carries_the_lifecycle()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "f");
        Backdate("f", createdDaysAgo: 30, usedDaysAgo: 1);

        var listed = (await ApiServer.Json(await server.Get(Flags))).EnumerateArray().Single();
        Assert.Equal("active", listed.GetProperty("lifecycle").GetString());
        Assert.Equal("active", (await ApiServer.Json(await server.Patch($"{Flags}/f", new { name = "F" }))).GetProperty("lifecycle").GetString());
        Assert.Equal("archived", (await ApiServer.Json(await server.Post($"{Flags}/f/archive"))).GetProperty("lifecycle").GetString());
        Assert.Equal("active", (await ApiServer.Json(await server.Post($"{Flags}/f/restore"))).GetProperty("lifecycle").GetString());
    }

    // --- delete ------------------------------------------------------------------------------------------------------

    private async Task Require(string flag, string on)
    {
        var path = $"{Flags}/{flag}/environments/prod";
        var current = await ApiServer.Json(await server.Get(path));
        var body = current.EnumerateObject().ToDictionary(p => p.Name, p => p.Value.Clone());
        body["prerequisites"] = JsonDocument.Parse($$"""[{"flagKey":"{{on}}","variation":0}]""").RootElement.Clone();
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put(path, body));
    }

    [Fact]
    public async Task Deleting_a_flag_that_is_not_archived_is_409_and_keeps_it()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "f");

        var response = await server.Delete($"{Flags}/f");

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", response);
        Assert.Equal("\"f\" is not archived; archive it before deleting it.", (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get($"{Flags}/f"));
        Assert.Single(server.Configs("f"));
    }

    [Theory]
    [InlineData(false)]
    [InlineData(true)]
    public async Task Deleting_a_flag_another_flag_requires_is_409_and_removes_nothing(bool dependentArchived)
    {
        await ProjectWithEnvironments("prod", "staging");
        await server.SeedFlag("shop", "base");
        await server.SeedFlag("shop", "b-dependent");
        await server.SeedFlag("shop", "a-dependent");
        await Require("b-dependent", "base");
        await Require("a-dependent", "base");
        await server.Post($"{Flags}/base/archive");
        if (dependentArchived)
        {
            await server.Post($"{Flags}/a-dependent/archive");
        }

        var response = await server.Delete($"{Flags}/base");

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", response);
        Assert.Equal(
            "\"base\" is a prerequisite of \"a-dependent\", \"b-dependent\"; remove it from them first.",
            (await ApiServer.Json(response)).GetProperty("error").GetProperty("message").GetString());
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get($"{Flags}/base"));
        Assert.Equal(2, server.Configs("base").Count);
        Assert.DoesNotContain(server.AuditRows(), r => r.Action == "deleted");
    }

    [Fact]
    public async Task Deleting_an_archived_flag_removes_it_and_its_configs_and_keeps_its_history()
    {
        await ProjectWithEnvironments("prod", "staging");
        await server.SeedFlag("shop", "f");
        await server.SeedFlag("shop", "other");
        Backdate("f", createdDaysAgo: 1, usedDaysAgo: 0);
        await server.Post($"{Flags}/f/archive");

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"{Flags}/f"));

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Flags}/f"));
        Assert.Empty(server.Configs("f"));
        Assert.Equal(2, server.Configs("other").Count);
        Assert.Equal(["other"], Keys(await ApiServer.Json(await server.Get($"{Flags}?archived=all"))));
        var deleted = server.AuditRows().Single(r => r is { EntityType: "flag", Action: "deleted" });
        Assert.Equal(("f", "shop", null, "admin"), (deleted.EntityId, deleted.Project, deleted.Environment, deleted.Actor));
        Assert.Equal("f", JsonDocument.Parse(deleted.Before!).RootElement.GetProperty("key").GetString());
        Assert.Null(deleted.After);
        var history = await ApiServer.Json(await server.Get($"{Flags}/f/history"));
        Assert.Contains(history.GetProperty("events").EnumerateArray(), e => e.GetProperty("action").GetString() == "deleted");
        Assert.Contains(history.GetProperty("events").EnumerateArray(), e => e.GetProperty("action").GetString() == "created");
    }

    [Fact]
    public async Task A_deleted_flags_key_can_be_created_again()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "f");
        await server.Post($"{Flags}/f/archive");
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"{Flags}/f"));

        var again = await server.Post(Flags, new { key = "f", name = "f again", variations = new[] { 1, 2, 3 } });

        await ApiServer.Expect(HttpStatusCode.Created, again);
        Assert.Equal("new", (await ApiServer.Json(again)).GetProperty("lifecycle").GetString());
        Assert.Single(server.Configs("f"));
    }

    [Fact]
    public async Task A_flag_that_lists_itself_does_not_block_its_own_delete()
    {
        await ProjectWithEnvironments("prod");
        await server.SeedFlag("shop", "f");
        await server.Post($"{Flags}/f/archive");
        // Validation refuses this through the API, so write the row as a stale or hand-edited database could hold it.
        Sql("UPDATE flag_environment_configs SET prerequisites = '[{\"flagKey\":\"f\",\"variation\":0}]'");

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"{Flags}/f"));
    }

    [Fact]
    public async Task Deleting_an_unknown_flag_or_project_is_404()
    {
        await ProjectWithEnvironments();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Delete($"{Flags}/missing"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Delete("/api/v1/projects/nope/flags/f"));
    }
}
