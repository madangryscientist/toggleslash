using System.Collections.Concurrent;
using System.Net.Http.Json;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Data;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Logging;

/// <summary>
/// The real server on a temp database, with one environment seeded and two extra routes, each gated on
/// one SDK policy and nothing else, so the policies are tested on their own. The real SDK routes are in
/// <c>SdkApiTests</c>.
/// </summary>
public sealed class AuthServer : IDisposable
{
    public const string AdminToken = "test-admin-token-not-a-real-secret";
    public const string KeysPath = "/api/v1/projects/shop/environments/prod/keys";
    public const string ServerOnlyRoute = "/test/server-only";
    public const string ClientOnlyRoute = "/test/client-only";

    private static readonly DateTime Now = new(2026, 11, 5, 12, 0, 0, DateTimeKind.Utc);

    public TempDatabase Database { get; } = new();
    public CapturedLogs Logs { get; } = new();
    public ServerOn Server { get; }
    public long ProdId { get; }
    public long StagingId { get; }

    /// <param name="adminToken">The admin token the server sees; null leaves the setting alone.</param>
    public AuthServer(string? adminToken = AdminToken)
    {
        Server = new ServerOn(Database.Path, builder =>
        {
            if (adminToken is not null)
            {
                builder.UseSetting(ServerAuth.AdminTokenSetting, adminToken);
            }
            builder.ConfigureLogging(logging => logging.SetMinimumLevel(LogLevel.Trace).AddProvider(Logs));
            builder.ConfigureServices(services => services.AddSingleton<IStartupFilter>(new SdkPolicyRoutes()));
        });

        using var scope = Server.Services.CreateScope();
        var context = scope.ServiceProvider.GetRequiredService<FeatureSlashDbContext>();
        var shop = new Project { Key = "shop", Name = "Shop", CreatedAt = Now };
        context.Add(shop);
        context.SaveChanges();
        var prod = new ProjectEnvironment { ProjectId = shop.Id, Key = "prod", Name = "Prod", CreatedAt = Now };
        var staging = new ProjectEnvironment { ProjectId = shop.Id, Key = "staging", Name = "Staging", CreatedAt = Now };
        context.AddRange(prod, staging);
        context.SaveChanges();
        ProdId = prod.Id;
        StagingId = staging.Id;
    }

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

    /// <summary>Stores a key straight in the database, as a test seed. Returns its plain key and id.</summary>
    public (string Plaintext, long Id) SeedKey(ApiKeyKind kind, long environmentId)
    {
        var secret = ApiKeySecret.Generate(kind);
        using var scope = Server.Services.CreateScope();
        var context = scope.ServiceProvider.GetRequiredService<FeatureSlashDbContext>();
        var key = new ApiKey
        {
            EnvironmentId = environmentId,
            Kind = kind,
            Name = "seeded",
            Hash = secret.Hash,
            Prefix = secret.Prefix,
            CreatedAt = Now,
        };
        context.Add(key);
        context.SaveChanges();
        return (secret.Plaintext, key.Id);
    }

    public void Dispose()
    {
        Server.Dispose();
        Database.Dispose();
    }

    private sealed class SdkPolicyRoutes : IStartupFilter
    {
        public Action<IApplicationBuilder> Configure(Action<IApplicationBuilder> next) => app =>
        {
            // A branch of its own, so every other request sees exactly the pipeline Program.cs builds.
            app.MapWhen(context => context.Request.Path.StartsWithSegments("/test"), branch =>
            {
                branch.UseRouting();
                branch.UseAuthentication();
                branch.UseAuthorization();
                branch.UseEndpoints(endpoints =>
                {
                    endpoints.MapGet(ServerOnlyRoute, Echo).RequireAuthorization(ServerAuth.ServerKeyPolicy);
                    endpoints.MapGet(ClientOnlyRoute, Echo).RequireAuthorization(ServerAuth.ClientKeyPolicy);
                });
            });
            next(app);
        };

        private static IResult Echo(HttpContext context) => Results.Json(new
        {
            principal = context.User.FindFirst(ServerAuth.PrincipalClaim)?.Value,
            environment = context.User.FindFirst(ServerAuth.EnvironmentClaim)?.Value,
        });
    }
}

public sealed class CapturedLogs : ILoggerProvider
{
    private readonly ConcurrentQueue<string> lines = new();

    public string All => string.Join('\n', lines);

    public ILogger CreateLogger(string categoryName) => new Logger(categoryName, lines);

    public void Dispose()
    {
    }

    private sealed class Logger(string category, ConcurrentQueue<string> lines) : ILogger
    {
        public IDisposable? BeginScope<TState>(TState state) where TState : notnull => null;

        public bool IsEnabled(LogLevel logLevel) => true;

        public void Log<TState>(LogLevel logLevel, EventId eventId, TState state, Exception? exception, Func<TState, Exception?, string> formatter) =>
            lines.Enqueue($"{category} {logLevel} {formatter(state, exception)} {exception}");
    }
}
