using System.Net;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Data;
using Microsoft.Extensions.DependencyInjection;

/// <summary>
/// Keys and tokens made before the <c>tgl_</c> prefix start with <c>fs_</c>. Authentication looks a credential up by the
/// hash of the whole text and never reads its prefix, so they keep working. These tests store rows the way an older
/// server did and send the old text.
/// </summary>
public sealed class LegacyPrefixTests : IDisposable
{
    private const string Project = "/api/v1/projects/shop";
    private static readonly DateTime Now = new(2026, 11, 5, 12, 0, 0, DateTimeKind.Utc);

    private readonly AuthServer server = new();

    public void Dispose() => server.Dispose();

    [Fact]
    public async Task An_sdk_key_made_with_the_old_prefix_still_authenticates()
    {
        var plaintext = "fs_server_" + new string('a', 64);
        Store(new ApiKey
        {
            EnvironmentId = server.ProdId,
            Kind = ApiKeyKind.Server,
            Name = "old",
            Hash = ApiKeySecret.HashOf(plaintext),
            Prefix = plaintext[..18],
            CreatedAt = Now,
        });

        var response = await server.Send(HttpMethod.Get, AuthServer.ServerOnlyRoute, $"Bearer {plaintext}");

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Equal(server.ProdId.ToString(), (await ApiServer.Json(response)).GetProperty("environment").GetString());
    }

    [Fact]
    public async Task A_scoped_admin_token_made_with_the_old_prefix_still_authenticates()
    {
        var plaintext = "fs_admin_" + new string('b', 64);
        long project;
        using (var scope = server.Server.Services.CreateScope())
        {
            project = scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>().Projects.Single(p => p.Key == "shop").Id;
        }
        Store(new AdminToken
        {
            ProjectId = project,
            Name = "old",
            Permission = AdminTokenPermission.Read,
            Hash = ApiKeySecret.HashOf(plaintext),
            Prefix = plaintext[..17],
            CreatedAt = Now,
            ExpiresAt = DateTime.UtcNow.AddDays(30),
        });

        var response = await server.Send(HttpMethod.Get, Project, $"Bearer {plaintext}");

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
    }

    private void Store(object entity)
    {
        using var scope = server.Server.Services.CreateScope();
        var context = scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>();
        context.Add(entity);
        context.SaveChanges();
    }
}
