using System.Net;
using System.Net.Http.Json;
using System.Text.Json;
using FeatureSlash.Server.Auth;
using Microsoft.AspNetCore.Hosting;

/// <summary>The real server on a temp database, driven through its own admin API.</summary>
public sealed class ApiServer : IDisposable
{
    public const string AdminToken = "test-admin-token-not-a-real-secret";

    private readonly TempDatabase database = new();

    public ServerOn Server { get; }

    public ApiServer()
        : this(null)
    {
    }

    private ApiServer(Action<IWebHostBuilder>? configure) =>
        Server = new ServerOn(database.Path, builder =>
        {
            builder.UseSetting(ServerAuth.AdminTokenSetting, AdminToken);
            configure?.Invoke(builder);
        });

    /// <summary>A factory, not a second public constructor: xunit refuses a class fixture that has two.</summary>
    public static ApiServer With(Action<IWebHostBuilder> configure) => new(configure);

    /// <summary>Sends the Authorization header exactly as given; null sends none.</summary>
    public async Task<HttpResponseMessage> Send(HttpMethod method, string path, string? authorization, object? body = null)
    {
        using var client = Server.CreateClient();
        using var request = new HttpRequestMessage(method, path);
        if (authorization is not null)
        {
            request.Headers.TryAddWithoutValidation("Authorization", authorization);
        }
        if (body is not null)
        {
            request.Content = JsonContent.Create(body);
        }
        return await client.SendAsync(request);
    }

    public Task<HttpResponseMessage> AsAdmin(HttpMethod method, string path, object? body = null) =>
        Send(method, path, $"Bearer {AdminToken}", body);

    public Task<HttpResponseMessage> Post(string path, object? body = null) => AsAdmin(HttpMethod.Post, path, body);

    public Task<HttpResponseMessage> Get(string path) => AsAdmin(HttpMethod.Get, path);

    public async Task<string> SeedProject(string key = "shop")
    {
        await Expect(HttpStatusCode.Created, await Post("/api/v1/projects", new { key, name = key }));
        return key;
    }

    public async Task SeedEnvironment(string project, string key) =>
        await Expect(HttpStatusCode.Created, await Post($"/api/v1/projects/{project}/environments", new { key, name = key }));

    public async Task SeedFlag(string project, string key, object[]? variations = null) =>
        await Expect(HttpStatusCode.Created, await Post($"/api/v1/projects/{project}/flags", new { key, name = key, variations = variations ?? [true, false] }));

    public static async Task<JsonElement> Json(HttpResponseMessage response) =>
        JsonDocument.Parse(await response.Content.ReadAsStringAsync()).RootElement.Clone();

    public static async Task Expect(HttpStatusCode status, HttpResponseMessage response) =>
        Assert.True(response.StatusCode == status, $"expected {status}, got {response.StatusCode}: {await response.Content.ReadAsStringAsync()}");

    /// <summary>The single error shape, with this code.</summary>
    public static async Task ExpectError(HttpStatusCode status, string code, HttpResponseMessage response)
    {
        var body = await Json(response);
        Assert.True(response.StatusCode == status, $"expected {status}, got {response.StatusCode}: {body}");
        var error = body.GetProperty("error");
        Assert.Equal(code, error.GetProperty("code").GetString());
        Assert.False(string.IsNullOrWhiteSpace(error.GetProperty("message").GetString()));
        Assert.Single(body.EnumerateObject());
    }

    /// <summary>Everything stored for a flag, read straight from the file as anything else would.</summary>
    public List<(string Environment, bool On, long? OffVariation, string Fallthrough)> Configs(string flagKey)
    {
        using var connection = database.Open();
        using var command = connection.CreateCommand();
        command.CommandText = """
            SELECT e.key, c."on", c.off_variation, c.fallthrough
            FROM flag_environment_configs c
            JOIN flags f ON f.id = c.flag_id
            JOIN environments e ON e.id = c.environment_id
            WHERE f.key = $key ORDER BY e.id
            """;
        command.Parameters.AddWithValue("$key", flagKey);
        using var reader = command.ExecuteReader();
        var rows = new List<(string, bool, long?, string)>();
        while (reader.Read())
        {
            rows.Add((reader.GetString(0), reader.GetBoolean(1), reader.IsDBNull(2) ? null : reader.GetInt64(2), reader.GetString(3)));
        }
        return rows;
    }

    public Task<HttpResponseMessage> Put(string path, object body) => AsAdmin(HttpMethod.Put, path, body);

    /// <summary>
    /// A flag's or a segment's salt, which the API never returns, read from the file: a test that evaluates
    /// a saved config needs it to build the ruleset the way an SDK read would.
    /// </summary>
    public string Salt(string table, string project, string key)
    {
        Assert.Contains(table, new[] { "flags", "segments" });
        using var connection = database.Open();
        using var command = connection.CreateCommand();
        command.CommandText = $"SELECT t.salt FROM {table} t JOIN projects p ON p.id = t.project_id WHERE p.key = $project AND t.key = $key";
        command.Parameters.AddWithValue("$project", project);
        command.Parameters.AddWithValue("$key", key);
        return (string)command.ExecuteScalar()!;
    }

    public Task<HttpResponseMessage> Patch(string path, object body) => AsAdmin(HttpMethod.Patch, path, body);

    public Task<HttpResponseMessage> Delete(string path) => AsAdmin(HttpMethod.Delete, path);

    /// <summary>A connection that bypasses the server, for a test that changes or reads the file as anything else could.</summary>
    public Microsoft.Data.Sqlite.SqliteConnection OpenDatabase() => database.Open();

    /// <summary>Every audit row, oldest first, read straight from the file.</summary>
    public List<AuditRow> AuditRows()
    {
        using var connection = database.Open();
        using var command = connection.CreateCommand();
        command.CommandText = """
            SELECT id, occurred_at, actor, entity_type, entity_id, action, project_key, environment_key, flag_key, "before", "after"
            FROM audit_events ORDER BY id
            """;
        using var reader = command.ExecuteReader();
        var rows = new List<AuditRow>();
        while (reader.Read())
        {
            string? Text(int i) => reader.IsDBNull(i) ? null : reader.GetString(i);
            rows.Add(new AuditRow(
                reader.GetInt64(0), reader.GetString(1), reader.GetString(2), reader.GetString(3), reader.GetString(4),
                reader.GetString(5), reader.GetString(6), Text(7), Text(8), Text(9), Text(10)));
        }
        return rows;
    }

    public void Dispose()
    {
        Server.Dispose();
        database.Dispose();
    }
}

public sealed record AuditRow(
    long Id,
    string OccurredAt,
    string Actor,
    string EntityType,
    string EntityId,
    string Action,
    string Project,
    string? Environment,
    string? Flag,
    string? Before,
    string? After)
{
    /// <summary>Every column joined: what a leak anywhere in the row would show up in.</summary>
    public string Everything => string.Join('\n', new[] { OccurredAt, Actor, EntityType, EntityId, Action, Project, Environment, Flag, Before, After });
}
