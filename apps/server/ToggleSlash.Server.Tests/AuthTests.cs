using System.Net;
using System.Text;
using System.Text.Json;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Data;

public sealed class AuthTests : IDisposable
{
    private const string WrongToken = "wrong-token-that-must-not-be-logged";

    private readonly AuthServer server = new();

    public void Dispose() => server.Dispose();

    [Theory]
    [InlineData(null)]
    [InlineData("Bearer ")]
    [InlineData("Bearer one two")]
    [InlineData("Basic dGVzdA==")]
    [InlineData(AuthServer.AdminToken)]
    [InlineData("Bearer " + WrongToken)]
    public async Task A_missing_malformed_or_wrong_token_is_401(string? authorization)
    {
        var response = await server.Send(HttpMethod.Get, AuthServer.KeysPath, authorization);

        await AssertError(response, HttpStatusCode.Unauthorized, "unauthorized");
        Assert.Equal("Bearer", response.Headers.WwwAuthenticate.Single().Scheme);
    }

    [Fact]
    public async Task Health_needs_no_database_even_with_a_token()
    {
        DropTheKeysTable();

        Assert.Equal(HttpStatusCode.OK, (await server.Send(HttpMethod.Get, "/health", "Bearer " + WrongToken)).StatusCode);
    }

    [Fact]
    public async Task A_key_resolves_to_its_environment_and_kind()
    {
        var (serverKey, _) = server.SeedKey(ApiKeyKind.Server, server.ProdId);
        var (client, _) = server.SeedKey(ApiKeyKind.Client, server.StagingId);

        var asServer = await Json(await server.Send(HttpMethod.Get, AuthServer.ServerOnlyRoute, $"Bearer {serverKey}"));
        var asClient = await Json(await server.Send(HttpMethod.Get, AuthServer.ClientOnlyRoute, $"Bearer {client}"));

        Assert.Equal("server-key", asServer.GetProperty("principal").GetString());
        Assert.Equal(server.ProdId.ToString(), asServer.GetProperty("environment").GetString());
        Assert.Equal("client-key", asClient.GetProperty("principal").GetString());
        Assert.Equal(server.StagingId.ToString(), asClient.GetProperty("environment").GetString());
    }

    [Theory]
    [InlineData(ApiKeyKind.Client, AuthServer.ServerOnlyRoute)]
    [InlineData(ApiKeyKind.Server, AuthServer.ClientOnlyRoute)]
    public async Task A_key_on_the_other_kinds_route_is_403(ApiKeyKind kind, string route)
    {
        var (key, _) = server.SeedKey(kind, server.ProdId);

        var response = await server.Send(HttpMethod.Get, route, $"Bearer {key}");

        await AssertError(response, HttpStatusCode.Forbidden, "forbidden");
    }

    [Theory]
    [InlineData(ApiKeyKind.Server)]
    [InlineData(ApiKeyKind.Client)]
    public async Task An_sdk_key_on_the_admin_api_is_403(ApiKeyKind kind)
    {
        var (key, _) = server.SeedKey(kind, server.ProdId);

        var response = await server.Send(HttpMethod.Get, AuthServer.KeysPath, $"Bearer {key}");

        await AssertError(response, HttpStatusCode.Forbidden, "forbidden");
    }

    [Fact]
    public async Task A_revoked_key_is_401_from_the_moment_it_is_revoked()
    {
        var created = await Json(await server.AsAdmin(HttpMethod.Post, AuthServer.KeysPath, new { kind = "server", name = "api" }));
        var key = created.GetProperty("key").GetString();
        var id = created.GetProperty("id").GetInt64();
        Assert.Equal(HttpStatusCode.OK, (await server.Send(HttpMethod.Get, AuthServer.ServerOnlyRoute, $"Bearer {key}")).StatusCode);

        var revoked = await server.AsAdmin(HttpMethod.Delete, $"{AuthServer.KeysPath}/{id}");

        Assert.Equal(HttpStatusCode.NoContent, revoked.StatusCode);
        var response = await server.Send(HttpMethod.Get, AuthServer.ServerOnlyRoute, $"Bearer {key}");
        await AssertError(response, HttpStatusCode.Unauthorized, "unauthorized");
    }

    [Fact]
    public async Task A_key_of_another_environment_cannot_be_revoked_through_this_one()
    {
        var (key, id) = server.SeedKey(ApiKeyKind.Server, server.StagingId);

        var response = await server.AsAdmin(HttpMethod.Delete, $"{AuthServer.KeysPath}/{id}");

        await AssertError(response, HttpStatusCode.NotFound, "not_found");
        Assert.Equal(HttpStatusCode.OK, (await server.Send(HttpMethod.Get, AuthServer.ServerOnlyRoute, $"Bearer {key}")).StatusCode);
    }

    [Theory]
    [InlineData("server")]
    [InlineData("client")]
    public async Task Creating_a_key_returns_the_plaintext_once_and_the_list_never_does(string kind)
    {
        var response = await server.AsAdmin(HttpMethod.Post, AuthServer.KeysPath, new { kind, name = "api" });

        Assert.Equal(HttpStatusCode.Created, response.StatusCode);
        var created = await Json(response);
        var plaintext = created.GetProperty("key").GetString()!;
        Assert.Matches($"^tgl_{kind}_[0-9a-f]{{64}}$", plaintext);
        Assert.Equal(kind, created.GetProperty("kind").GetString());
        Assert.Equal("api", created.GetProperty("name").GetString());
        Assert.Equal(plaintext[..($"tgl_{kind}_".Length + 8)], created.GetProperty("prefix").GetString());

        var listResponse = await server.AsAdmin(HttpMethod.Get, AuthServer.KeysPath);
        var raw = await listResponse.Content.ReadAsStringAsync();

        Assert.DoesNotContain(plaintext, raw);
        Assert.DoesNotContain(ApiKeySecret.HashOf(plaintext), raw);
        var listed = JsonDocument.Parse(raw).RootElement.EnumerateArray().Single();
        Assert.Equal(
            ["createdAt", "id", "kind", "name", "prefix", "revokedAt"],
            listed.EnumerateObject().Select(p => p.Name).Order());
    }

    [Fact]
    public async Task The_plaintext_is_not_in_the_database()
    {
        var created = await Json(await server.AsAdmin(HttpMethod.Post, AuthServer.KeysPath, new { kind = "server", name = "api" }));
        var plaintext = created.GetProperty("key").GetString()!;

        // Every file SQLite keeps, the write-ahead log included, because the row may not be checkpointed yet.
        var files = await server.Database.ReadEveryFile();
        Assert.NotEmpty(files);
        var hashSeen = false;
        foreach (var bytes in files)
        {
            var contents = Encoding.Latin1.GetString(bytes);
            Assert.DoesNotContain(plaintext, contents);
            hashSeen |= contents.Contains(ApiKeySecret.HashOf(plaintext));
        }
        // Proves the scan looks at the bytes the row went to.
        Assert.True(hashSeen);
    }

    [Fact]
    public async Task No_token_reaches_the_logs()
    {
        var created = await Json(await server.AsAdmin(HttpMethod.Post, AuthServer.KeysPath, new { kind = "server", name = "api" }));
        var plaintext = created.GetProperty("key").GetString()!;
        var id = created.GetProperty("id").GetInt64();

        await server.Send(HttpMethod.Get, AuthServer.ServerOnlyRoute, $"Bearer {plaintext}");
        await server.Send(HttpMethod.Get, AuthServer.ClientOnlyRoute, $"Bearer {plaintext}");
        await server.Send(HttpMethod.Get, AuthServer.KeysPath, $"Bearer {WrongToken}");
        await server.AsAdmin(HttpMethod.Delete, $"{AuthServer.KeysPath}/{id}");
        await server.Send(HttpMethod.Get, AuthServer.ServerOnlyRoute, $"Bearer {plaintext}");

        var logs = server.Logs.All;
        // Proves the capture sees this traffic at the most verbose level, database commands included.
        Assert.Contains("AuthenticationScheme: bearer was challenged", logs);
        Assert.Contains("api_keys", logs);
        Assert.DoesNotContain(plaintext, logs);
        Assert.DoesNotContain(plaintext[("tgl_server_".Length + 8)..], logs);
        Assert.DoesNotContain(ApiKeySecret.HashOf(plaintext), logs);
        Assert.DoesNotContain(AuthServer.AdminToken, logs);
        Assert.DoesNotContain(WrongToken, logs);
    }

    [Theory]
    [InlineData("{\"kind\":\"admin\",\"name\":\"api\"}")]
    [InlineData("{\"kind\":\"server\",\"name\":\"  \"}")]
    [InlineData("not json")]
    public async Task A_bad_create_body_is_400_and_stores_nothing(string body)
    {
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Post, AuthServer.KeysPath)
        {
            Content = new StringContent(body, Encoding.UTF8, "application/json"),
        };
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {AuthServer.AdminToken}");

        var response = await client.SendAsync(request);

        await AssertError(response, HttpStatusCode.BadRequest, "bad_request");
        Assert.Empty((await Json(await server.AsAdmin(HttpMethod.Get, AuthServer.KeysPath))).EnumerateArray());
    }

    [Theory]
    [InlineData("/api/v1/projects/nope/environments/prod/keys")]
    [InlineData("/api/v1/projects/shop/environments/nope/keys")]
    public async Task An_unknown_project_or_environment_is_404(string path)
    {
        await AssertError(await server.AsAdmin(HttpMethod.Get, path), HttpStatusCode.NotFound, "not_found");
    }

    [Fact]
    public async Task Framework_errors_have_the_same_shape()
    {
        await AssertError(await server.Send(HttpMethod.Get, "/nope", null), HttpStatusCode.NotFound, "not_found");
        await AssertError(await server.AsAdmin(HttpMethod.Put, AuthServer.KeysPath), HttpStatusCode.MethodNotAllowed, "method_not_allowed");
    }

    [Fact]
    public async Task An_unhandled_failure_has_the_same_shape_and_leaks_nothing()
    {
        DropTheKeysTable();

        var response = await server.Send(HttpMethod.Get, AuthServer.KeysPath, "Bearer " + WrongToken);

        await AssertError(response, HttpStatusCode.InternalServerError, "internal_server_error");
        Assert.DoesNotContain("api_keys", await response.Content.ReadAsStringAsync());
    }

    private void DropTheKeysTable()
    {
        using var connection = server.Database.Open();
        using var command = connection.CreateCommand();
        command.CommandText = "DROP TABLE api_keys";
        command.ExecuteNonQuery();
    }

    internal static async Task AssertError(HttpResponseMessage response, HttpStatusCode status, string code)
    {
        Assert.Equal(status, response.StatusCode);
        Assert.Equal("application/json; charset=utf-8", response.Content.Headers.ContentType?.ToString());
        var root = await Json(response);
        Assert.Equal("error", root.EnumerateObject().Single().Name);
        var error = root.GetProperty("error");
        Assert.Equal(["code", "message"], error.EnumerateObject().Select(p => p.Name).Order());
        Assert.Equal(code, error.GetProperty("code").GetString());
        Assert.False(string.IsNullOrWhiteSpace(error.GetProperty("message").GetString()));
    }

    private static async Task<JsonElement> Json(HttpResponseMessage response) =>
        JsonDocument.Parse(await response.Content.ReadAsStringAsync()).RootElement;
}
