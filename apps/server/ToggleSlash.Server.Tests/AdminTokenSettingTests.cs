using System.Net;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Data;

/// <summary>Sets a process-wide variable, so these do not run alongside anything else.</summary>
[CollectionDefinition(nameof(AdminTokenSettingTests), DisableParallelization = true)]
public sealed class AdminTokenSettingCollection;

[Collection(nameof(AdminTokenSettingTests))]
public sealed class AdminTokenSettingTests : IDisposable
{
    private readonly string? original = Environment.GetEnvironmentVariable(ServerAuth.AdminTokenSetting);

    public void Dispose() => Environment.SetEnvironmentVariable(ServerAuth.AdminTokenSetting, original);

    [Fact]
    public async Task The_token_comes_from_the_environment_variable()
    {
        Environment.SetEnvironmentVariable(ServerAuth.AdminTokenSetting, "token-from-the-environment");
        using var server = new AuthServer(adminToken: null);

        var response = await server.Send(HttpMethod.Get, AuthServer.KeysPath, "Bearer token-from-the-environment");

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
    }

    [Theory]
    [InlineData(null)]
    [InlineData("Bearer anything")]
    public async Task Unset_refuses_every_admin_request_with_503(string? authorization)
    {
        Environment.SetEnvironmentVariable(ServerAuth.AdminTokenSetting, null);
        using var server = new AuthServer(adminToken: null);

        var response = await server.Send(HttpMethod.Get, AuthServer.KeysPath, authorization);

        Assert.Contains(ServerAuth.AdminTokenSetting, await response.Content.ReadAsStringAsync());
        await AuthTests.AssertError(response, HttpStatusCode.ServiceUnavailable, "admin_token_unset");
    }

    [Fact]
    public async Task Unset_turns_off_the_admin_api_and_nothing_else()
    {
        Environment.SetEnvironmentVariable(ServerAuth.AdminTokenSetting, null);
        using var server = new AuthServer(adminToken: null);
        var (key, _) = server.SeedKey(ApiKeyKind.Server, server.ProdId);

        Assert.Equal(HttpStatusCode.OK, (await server.Send(HttpMethod.Get, AuthServer.ServerOnlyRoute, $"Bearer {key}")).StatusCode);
    }
}
