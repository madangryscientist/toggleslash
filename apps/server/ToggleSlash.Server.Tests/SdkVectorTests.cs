using System.Net;
using System.Text.Json;
using ToggleSlash.Server.Data;
using Microsoft.Extensions.DependencyInjection;

/// <summary>
/// The shared vectors in <c>fixtures/evaluation/</c>, run through the server: each vector's ruleset is stored in
/// the database as the admin API would store it, and <c>POST /sdk/v1/evaluate</c> must answer what the vector
/// expects. That covers the server's ruleset read, its JSON and the evaluator together.
/// </summary>
/// <remarks>
/// <c>errors.json</c> is left out on purpose: its flags are malformed, which the admin API refuses to save, so
/// the database can never hold them. The evaluator's own tests run that file. Its INVALID_CONTEXT cases are
/// answered as a 400 by the server and are covered in <see cref="SdkApiTests"/>.
/// </remarks>
public sealed class SdkVectorTests(ApiServer server) : IClassFixture<ApiServer>
{
    // One server for every vector: each vector stores its ruleset in a project of its own.

    /// <summary>
    /// Vectors whose ruleset cannot exist in the server: the admin API refuses to save it (malformed, duplicate
    /// keys) or UTF-8 JSON over HTTP cannot carry it (a lone surrogate). The evaluator's own tests run them.
    /// </summary>
    private static readonly Dictionary<string, string> Unstorable = new()
    {
        ["malformed prerequisite is an error"] = "a malformed prerequisite is refused on save",
        ["malformed segment: included is not a list of strings"] = "a malformed segment is refused on save",
        ["malformed segment: no salt"] = "a segment always has a salt",
        ["order: first bad reference decides the code"] = "it holds a malformed rule, refused on save",
        ["duplicate segment keys: the first wins"] = "segment keys are unique per environment",
        ["strings: a lone surrogate in a context key is kept, not replaced"] = "a lone surrogate is not valid UTF-8 JSON",
        ["in: a lone surrogate is not U+FFFD"] = "a lone surrogate is not valid UTF-8 JSON",
    };

    public static IEnumerable<object[]> StorableVectors() =>
        Fixtures.VectorFiles.Where(file => file != "errors.json").SelectMany(file =>
            Fixtures.Vectors(file).EnumerateArray()
                .Select(vector => vector.GetProperty("name").GetString()!)
                .Where(name => !Unstorable.ContainsKey(name))
                .Select(name => new object[] { file, name }));

    [Fact]
    public void Every_excluded_vector_exists()
    {
        var names = Fixtures.VectorFiles.SelectMany(file => Fixtures.Vectors(file).EnumerateArray())
            .Select(vector => vector.GetProperty("name").GetString()!)
            .ToHashSet();

        Assert.All(Unstorable.Keys, name => Assert.Contains(name, names));
    }

    [Theory]
    [MemberData(nameof(StorableVectors))]
    public async Task Evaluate_agrees_with_the_vector(string file, string name)
    {
        var vector = Fixtures.Vector(file, name);
        var flagKey = vector.GetProperty("flagKey").GetString()!;
        var token = await Store(vector);

        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Post, "/sdk/v1/evaluate")
        {
            Content = JsonContent(new { flagKey, context = vector.GetProperty("context") }),
        };
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {token}");
        var response = await client.SendAsync(request);

        await ApiServer.Expect(HttpStatusCode.OK, response);
        var actual = (await ApiServer.Json(response)).GetProperty("flags").GetProperty(flagKey);
        var expected = vector.GetProperty("expected");
        Assert.True(JsonElement.DeepEquals(expected.GetProperty("value"), actual.GetProperty("value")), $"value: expected {expected.GetProperty("value")}, got {actual.GetProperty("value")}");
        Assert.True(JsonElement.DeepEquals(expected.GetProperty("variationIndex"), actual.GetProperty("variationIndex")), "variationIndex");
        Assert.True(JsonElement.DeepEquals(expected.GetProperty("reason"), actual.GetProperty("reason")), $"reason: expected {expected.GetProperty("reason")}, got {actual.GetProperty("reason")}");
    }

    private static StringContent JsonContent(object body) =>
        new(JsonSerializer.Serialize(body), System.Text.Encoding.UTF8, "application/json");

    private static int project;

    /// <summary>A project and environment of their own, the vector's flags and segments stored, and a client key for it.</summary>
    private async Task<string> Store(JsonElement vector)
    {
        var key = $"vector-{Interlocked.Increment(ref project)}";
        await server.SeedProject(key);
        await server.SeedEnvironment(key, "prod");

        using (var scope = server.Server.Services.CreateScope())
        {
            var database = scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>();
            var projectId = database.Projects.Single(p => p.Key == key).Id;
            var environmentId = database.Environments.Single(e => e.ProjectId == projectId).Id;
            var now = DateTime.UtcNow;

            foreach (var flag in vector.GetProperty("flags").EnumerateArray())
            {
                var stored = new Flag
                {
                    ProjectId = projectId,
                    Key = flag.GetProperty("key").GetString()!,
                    Name = flag.GetProperty("key").GetString()!,
                    Variations = flag.GetProperty("variations").GetRawText(),
                    Salt = flag.GetProperty("salt").GetString()!,
                    CreatedAt = now,
                };
                database.Flags.Add(stored);
                await database.SaveChangesAsync();
                database.FlagEnvironmentConfigs.Add(new FlagEnvironmentConfig
                {
                    FlagId = stored.Id,
                    EnvironmentId = environmentId,
                    On = flag.GetProperty("on").GetBoolean(),
                    OffVariation = flag.TryGetProperty("offVariation", out var off) && off.ValueKind == JsonValueKind.Number ? off.GetInt32() : null,
                    Targets = flag.GetProperty("targets").GetRawText(),
                    Prerequisites = flag.GetProperty("prerequisites").GetRawText(),
                    Rules = flag.GetProperty("rules").GetRawText(),
                    Fallthrough = flag.GetProperty("fallthrough").GetRawText(),
                    Version = flag.GetProperty("version").GetInt64(),
                });
            }
            foreach (var segment in vector.GetProperty("segments").EnumerateArray())
            {
                database.Segments.Add(new Segment
                {
                    ProjectId = projectId,
                    EnvironmentId = environmentId,
                    Key = segment.GetProperty("key").GetString()!,
                    Salt = segment.GetProperty("salt").GetString()!,
                    Included = segment.GetProperty("included").GetRawText(),
                    Excluded = segment.GetProperty("excluded").GetRawText(),
                    Rules = segment.GetProperty("rules").GetRawText(),
                });
            }
            await database.SaveChangesAsync();
        }

        var created = await server.Post($"/api/v1/projects/{key}/environments/prod/keys", new { kind = "client", name = "vector" });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }
}
