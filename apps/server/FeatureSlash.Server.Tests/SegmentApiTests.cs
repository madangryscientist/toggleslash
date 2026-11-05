using System.Net;
using System.Net.Http.Json;
using System.Text.Json;

/// <summary>Segments belong to one environment: CRUD, validation, and a delete that refuses while a rule still names the segment.</summary>
public sealed class SegmentApiTests : IDisposable
{
    private const string Prod = "/api/v1/projects/shop/environments/prod/segments";
    private const string Staging = "/api/v1/projects/shop/environments/staging/segments";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("shop", "staging");
    }

    private static object Segment(string key = "beta", string[]? included = null, string[]? excluded = null, object[]? rules = null) =>
        new { key, included = included ?? [], excluded = excluded ?? [], rules = rules ?? [] };

    private static Task ExpectBadRequest(HttpResponseMessage response) => ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", response);

    private Task<HttpResponseMessage> RawPost(string path, string json) => Raw(HttpMethod.Post, path, json);

    private async Task<HttpResponseMessage> Raw(HttpMethod method, string path, string json, string? ifMatch = null)
    {
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(method, path) { Content = new StringContent(json, System.Text.Encoding.UTF8, "application/json") };
        request.Headers.Authorization = new System.Net.Http.Headers.AuthenticationHeaderValue("Bearer", ApiServer.AdminToken);
        if (ifMatch is not null)
        {
            request.Headers.TryAddWithoutValidation("If-Match", ifMatch);
        }
        return await client.SendAsync(request);
    }

    [Fact]
    public async Task Create_returns_the_segment_with_a_location_and_it_reads_back()
    {
        await Seed();
        var rules = new object[] { new { clauses = new[] { new { attribute = "plan", op = "in", values = new[] { "pro" } } }, weight = 50000 } };

        var created = await server.Post(Prod, Segment("beta", ["alice"], ["mallory"], rules));

        await ApiServer.Expect(HttpStatusCode.Created, created);
        Assert.Equal($"{Prod}/beta", created.Headers.Location!.OriginalString);
        var body = await ApiServer.Json(created);
        Assert.Equal("beta", body.GetProperty("key").GetString());
        Assert.Equal("""["alice"]""", body.GetProperty("included").GetRawText());
        Assert.Equal("""["mallory"]""", body.GetProperty("excluded").GetRawText());
        Assert.Equal("""[{"clauses":[{"attribute":"plan","op":"in","values":["pro"]}],"weight":50000}]""", body.GetProperty("rules").GetRawText());
        Assert.False(body.TryGetProperty("salt", out _));
        Assert.Equal(body.GetRawText(), (await ApiServer.Json(await server.Get($"{Prod}/beta"))).GetRawText());
    }

    [Fact]
    public async Task List_is_this_environments_segments_by_key()
    {
        await Seed();
        foreach (var key in new[] { "zeta", "alpha" })
        {
            await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment(key)));
        }
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Staging, Segment("other")));

        var prod = await ApiServer.Json(await server.Get(Prod));
        var staging = await ApiServer.Json(await server.Get(Staging));

        Assert.Equal(["alpha", "zeta"], prod.EnumerateArray().Select(s => s.GetProperty("key").GetString()));
        Assert.Equal(["other"], staging.EnumerateArray().Select(s => s.GetProperty("key").GetString()));
    }

    [Fact]
    public async Task The_same_key_in_two_environments_is_two_segments_and_in_one_is_409()
    {
        await Seed();

        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment()));
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Staging, Segment()));
        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", await server.Post(Prod, Segment()));
    }

    [Theory]
    [InlineData("nope", "prod", "beta")]
    [InlineData("shop", "nope", "beta")]
    [InlineData("shop", "prod", "nope")]
    public async Task An_unknown_project_environment_or_segment_is_404(string project, string env, string segment)
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment()));
        var path = $"/api/v1/projects/{project}/environments/{env}/segments/{segment}";

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get(path));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Put(path, new { included = new string[0], excluded = new string[0], rules = new object[0] }));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.AsAdmin(HttpMethod.Delete, path));
    }

    [Fact]
    public async Task Put_replaces_members_and_rules()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment("beta", ["alice"], ["bob"])));

        var put = await server.Put($"{Prod}/beta", new { included = new[] { "carol" }, excluded = new string[0], rules = new[] { new { clauses = new object[0] } }, version = 1 });

        await ApiServer.Expect(HttpStatusCode.OK, put);
        var read = await ApiServer.Json(await server.Get($"{Prod}/beta"));
        Assert.Equal("""["carol"]""", read.GetProperty("included").GetRawText());
        Assert.Equal("[]", read.GetProperty("excluded").GetRawText());
        Assert.Equal("""[{"clauses":[]}]""", read.GetProperty("rules").GetRawText());
    }

    private static object Members(long? version, params string[] included) =>
        new { included, excluded = new string[0], rules = new object[0], version };

    private async Task<long> Version(string key = "beta") =>
        (await ApiServer.Json(await server.Get($"{Prod}/{key}"))).GetProperty("version").GetInt64();

    private List<AuditRow> SegmentUpdates() =>
        [.. server.AuditRows().Where(r => r.EntityType == "segment" && r.Action == "updated")];

    [Fact]
    public async Task A_segment_starts_at_version_1_and_a_save_adds_1_and_is_audited()
    {
        await Seed();

        var created = await server.Post(Prod, Segment("beta", ["alice"]));
        Assert.Equal(1, (await ApiServer.Json(created)).GetProperty("version").GetInt64());

        var first = await server.Put($"{Prod}/beta", Members(1, "bob"));
        await ApiServer.Expect(HttpStatusCode.OK, first);
        Assert.Equal(2, (await ApiServer.Json(first)).GetProperty("version").GetInt64());
        Assert.Equal("\"2\"", first.Headers.ETag!.Tag);
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put($"{Prod}/beta", Members(2, "carol")));

        Assert.Equal(3, await Version());
        Assert.Equal(
            [(1L, 2L), (2L, 3L)],
            SegmentUpdates().Select(r => (JsonDocument.Parse(r.Before!).RootElement.GetProperty("version").GetInt64(), JsonDocument.Parse(r.After!).RootElement.GetProperty("version").GetInt64())));
        Assert.Contains("carol", SegmentUpdates()[^1].After);
    }

    [Fact]
    public async Task Two_concurrent_saves_from_one_version_get_one_save_and_one_409()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment("beta", ["original"])));

        var responses = await Task.WhenAll(Enumerable.Range(0, 2).Select(i => server.Put($"{Prod}/beta", Members(1, $"writer-{i}"))));

        var winner = Assert.Single(responses, r => r.StatusCode == HttpStatusCode.OK);
        foreach (var loser in responses.Where(r => r != winner))
        {
            await ApiServer.ExpectError(HttpStatusCode.Conflict, "version_conflict", loser);
        }
        var read = await ApiServer.Json(await server.Get($"{Prod}/beta"));
        Assert.Equal(2, read.GetProperty("version").GetInt64());
        Assert.Equal((await ApiServer.Json(winner)).GetProperty("included").GetRawText(), read.GetProperty("included").GetRawText());
        Assert.Single(SegmentUpdates());
    }

    [Fact]
    public async Task A_put_needs_a_version_in_the_body_or_in_if_match()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment("beta", ["alice"])));
        var read = await server.Get($"{Prod}/beta");
        Assert.Equal("\"1\"", read.Headers.ETag!.Tag);
        const string Body = """{"included":["bob"],"excluded":[],"rules":[]}""";

        await ExpectBadRequest(await Raw(HttpMethod.Put, $"{Prod}/beta", Body));
        Assert.Equal(1, await Version());

        await ApiServer.Expect(HttpStatusCode.OK, await Raw(HttpMethod.Put, $"{Prod}/beta", Body, read.Headers.ETag.Tag));
        Assert.Equal(2, await Version());
    }

    public static TheoryData<string> InvalidBodies => new()
    {
        """{"key":"","included":[],"excluded":[],"rules":[]}""",
        """{"key":"Has Space","included":[],"excluded":[],"rules":[]}""",
        """{"included":[],"excluded":[],"rules":[]}""",
        """{"key":"s","excluded":[],"rules":[]}""",
        """{"key":"s","included":[],"rules":[]}""",
        """{"key":"s","included":[],"excluded":[]}""",
        """{"key":"s","included":[1],"excluded":[],"rules":[]}""",
        """{"key":"s","included":[""],"excluded":[],"rules":[]}""",
        """{"key":"s","included":[],"excluded":[null],"rules":[]}""",
        """{"key":"s","included":[],"excluded":[],"rules":{}}""",
        """{"key":"s","included":[],"excluded":[],"rules":[],"colour":"red"}""",
        """{"key":"s","included":[],"excluded":[],"rules":[{"weight":10}]}""",
        """{"key":"s","included":[],"excluded":[],"rules":[{"clauses":[],"weight":100001}]}""",
        """{"key":"s","included":[],"excluded":[],"rules":[{"clauses":[],"weight":-1}]}""",
        """{"key":"s","included":[],"excluded":[],"rules":[{"clauses":[],"weight":0.5}]}""",
        """{"key":"s","included":[],"excluded":[],"rules":[{"clauses":[{"attribute":"a","op":"nope","values":["x"]}]}]}""",
        """{"key":"s","included":[],"excluded":[],"rules":[{"clauses":[{"op":"segmentMatch","values":["other"]}]}]}""",
        """{"key":"s","included":[],"excluded":[],"rules":[{"clauses":[],"variation":0}]}""",
        """{"key":"s","included":[],"excluded":[],"rules":[{"clauses":[{"attribute":"\ud800","op":"in","values":["x"]}]}]}""",
    };

    [Theory]
    [MemberData(nameof(InvalidBodies))]
    public async Task An_invalid_body_is_400_and_creates_nothing(string json)
    {
        await Seed();

        await ExpectBadRequest(await RawPost(Prod, json));

        Assert.Equal(0, (await ApiServer.Json(await server.Get(Prod))).GetArrayLength());
    }

    [Fact]
    public async Task An_invalid_put_is_400_and_changes_nothing()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment("beta", ["alice"])));

        await ExpectBadRequest(await Raw(HttpMethod.Put, $"{Prod}/beta", """{"included":[],"excluded":[],"rules":[{"clauses":[{"op":"segmentMatch","values":["x"]}]}]}"""));
        await ExpectBadRequest(await Raw(HttpMethod.Put, $"{Prod}/beta", """{"included":[],"excluded":[]}"""));

        Assert.Equal("""["alice"]""", (await ApiServer.Json(await server.Get($"{Prod}/beta"))).GetProperty("included").GetRawText());
    }

    [Fact]
    public async Task Delete_removes_an_unused_segment()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment()));

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.AsAdmin(HttpMethod.Delete, $"{Prod}/beta"));

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get($"{Prod}/beta"));
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment()));
    }

    [Fact]
    public async Task Delete_is_409_while_a_flags_rule_names_the_segment_and_works_once_it_does_not()
    {
        await Seed();
        await server.SeedFlag("shop", "dark-mode");
        await server.SeedFlag("shop", "checkout");
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment()));
        var config = "/api/v1/projects/shop/flags/dark-mode/environments/prod";
        var put = await server.Put(config, new
        {
            on = true,
            offVariation = 1,
            targets = new object[0],
            prerequisites = new object[0],
            rules = new[] { new { clauses = new[] { new { op = "segmentMatch", values = new[] { "beta" } } }, variation = 0 } },
            fallthrough = new { variation = 1 },
            version = 1,
        });
        await ApiServer.Expect(HttpStatusCode.OK, put);

        var refused = await server.AsAdmin(HttpMethod.Delete, $"{Prod}/beta");

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", refused);
        Assert.Contains("dark-mode", (await ApiServer.Json(refused)).GetProperty("error").GetProperty("message").GetString());
        await ApiServer.Expect(HttpStatusCode.OK, await server.Get($"{Prod}/beta"));

        await ApiServer.Expect(HttpStatusCode.OK, await server.Put(config, new
        {
            on = true,
            offVariation = 1,
            targets = new object[0],
            prerequisites = new object[0],
            rules = new object[0],
            fallthrough = new { variation = 1 },
            version = 2,
        }));
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.AsAdmin(HttpMethod.Delete, $"{Prod}/beta"));
    }

    [Fact]
    public async Task A_rule_in_another_environment_does_not_block_the_delete()
    {
        await Seed();
        await server.SeedFlag("shop", "dark-mode");
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment()));
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Staging, Segment()));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put("/api/v1/projects/shop/flags/dark-mode/environments/staging", new
        {
            on = true,
            offVariation = 1,
            targets = new object[0],
            prerequisites = new object[0],
            rules = new[] { new { clauses = new[] { new { op = "segmentMatch", values = new[] { "beta" } } }, variation = 0 } },
            fallthrough = new { variation = 1 },
            version = 1,
        }));

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.AsAdmin(HttpMethod.Delete, $"{Prod}/beta"));
        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", await server.AsAdmin(HttpMethod.Delete, $"{Staging}/beta"));
    }

    [Fact]
    public async Task A_negated_segment_match_still_counts_as_a_use()
    {
        await Seed();
        await server.SeedFlag("shop", "dark-mode");
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment()));
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(Prod, Segment("other")));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Put("/api/v1/projects/shop/flags/dark-mode/environments/prod", new
        {
            on = true,
            offVariation = 1,
            targets = new object[0],
            prerequisites = new object[0],
            rules = new[] { new { clauses = new object[] { new { op = "segmentMatch", values = new[] { "other", "beta" }, negate = true } }, variation = 0 } },
            fallthrough = new { variation = 1 },
            version = 1,
        }));

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", await server.AsAdmin(HttpMethod.Delete, $"{Prod}/beta"));
    }
}
