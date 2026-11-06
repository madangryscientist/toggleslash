using System.Net;

/// <summary>Every catalogue route is admin only: no token and a wrong one are 401, an SDK key is 403.</summary>
public sealed class CatalogueAuthTests : IDisposable
{
    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    public static TheoryData<string, string, bool> Routes => new()
    {
        { "POST", "/api/v1/projects", true },
        { "GET", "/api/v1/projects", false },
        { "GET", "/api/v1/projects/shop", false },
        { "POST", "/api/v1/projects/shop/tokens", true },
        { "GET", "/api/v1/projects/shop/tokens", false },
        { "DELETE", "/api/v1/projects/shop/tokens/1", false },
        { "POST", "/api/v1/projects/shop/environments", true },
        { "GET", "/api/v1/projects/shop/environments", false },
        { "GET", "/api/v1/projects/shop/environments/prod", false },
        { "POST", "/api/v1/projects/shop/flags", true },
        { "GET", "/api/v1/projects/shop/flags", false },
        { "GET", "/api/v1/projects/shop/flags/dark-mode", false },
        { "PATCH", "/api/v1/projects/shop/flags/dark-mode", true },
        { "POST", "/api/v1/projects/shop/flags/dark-mode/archive", false },
        { "POST", "/api/v1/projects/shop/flags/dark-mode/restore", false },
        { "GET", "/api/v1/projects/shop/flags/dark-mode/environments/prod", false },
        { "GET", "/api/v1/projects/shop/environments/prod/flag-states", false },
        { "PUT", "/api/v1/projects/shop/flags/dark-mode/environments/prod", true },
        { "POST", "/api/v1/projects/shop/flags/dark-mode/environments/prod/toggle", true },
        { "GET", "/api/v1/projects/shop/environments/prod/rulesets/abc", false },
        { "POST", "/api/v1/projects/shop/environments/prod/segments", true },
        { "GET", "/api/v1/projects/shop/environments/prod/segments", false },
        { "GET", "/api/v1/projects/shop/environments/prod/segments/beta", false },
        { "PUT", "/api/v1/projects/shop/environments/prod/segments/beta", true },
        { "DELETE", "/api/v1/projects/shop/environments/prod/segments/beta", false },
    };

    [Theory]
    [MemberData(nameof(Routes))]
    public async Task No_token_is_401(string method, string path, bool hasBody)
    {
        var response = await server.Send(new HttpMethod(method), path, null, hasBody ? new { } : null);

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", response);
    }

    [Theory]
    [MemberData(nameof(Routes))]
    public async Task A_wrong_token_is_401(string method, string path, bool hasBody)
    {
        var response = await server.Send(new HttpMethod(method), path, "Bearer not-the-admin-token", hasBody ? new { } : null);

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", response);
    }

    [Theory]
    [MemberData(nameof(Routes))]
    public async Task An_sdk_key_is_403(string method, string path, bool hasBody)
    {
        await server.SeedProject("sdk");
        await server.SeedEnvironment("sdk", "prod");
        var created = await ApiServer.Json(await server.Post("/api/v1/projects/sdk/environments/prod/keys", new { kind = "server", name = "k" }));
        var key = created.GetProperty("key").GetString();

        var response = await server.Send(new HttpMethod(method), path, $"Bearer {key}", hasBody ? new { } : null);

        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", response);
    }
}
