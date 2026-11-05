using System.Reflection;
using System.Text.Json.Serialization;
using FeatureSlash.Server.Audit;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Catalogue;
using FeatureSlash.Server.Data;
using FeatureSlash.Server.Hardening;
using FeatureSlash.Server.Limits;
using FeatureSlash.Server.Sdk;
using FeatureSlash.Server.Targeting;
using FeatureSlash.Server.Usage;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddServerDatabase();
builder.Services.AddServerAuth();
builder.Services.AddRateLimits();
builder.AddRequestLimits();
// This middleware names an unlisted proxy's address at Debug, and no client IP goes in a log at any level.
builder.Logging.AddFilter("Microsoft.AspNetCore.HttpOverrides", LogLevel.None);
builder.Services.AddServerOpenApi();
builder.Services.AddSdkApi();
builder.Services.AddUsage();
builder.Services.AddSingleton<TargetingLock>();
// A number is a JSON number: "1" for an index is a 400, and the OpenAPI document says integer, not "integer or string".
builder.Services.ConfigureHttpJsonOptions(options => options.SerializerOptions.NumberHandling = JsonNumberHandling.Strict);
// An unreadable body is the client's 400 in every environment, not a Development-only exception.
builder.Services.Configure<RouteHandlerOptions>(options => options.ThrowOnBadRequest = false);
var app = builder.Build();

// Before the migration, so a bad limit or proxy list stops the server without touching the database. Headers go
// first, so every answer below carries them; trusted proxies next, so everything after sees the client's address.
_ = app.Services.GetRequiredService<RequestLimiter>();
app.UseSecurityHeaders();
app.UseTrustedProxies();

app.Services.MigrateServerDatabase();

// Explicit, in this order: left to the host, authentication would run outside the error handling.
app.UseExceptionHandler(new ExceptionHandlerOptions
{
    ExceptionHandler = context =>
        ApiError.Write(context, StatusCodes.Status500InternalServerError, "internal_server_error", "Internal Server Error"),
});
app.UseStatusCodePages(context => ApiError.WriteForStatus(context.HttpContext));
// Routing runs first without being asked (a WebApplication puts UseRouting at the start of the pipeline), so the
// endpoint is known here and a route's own timeout, or its opt-out, is read. Inside the exception handler, so the
// cancellation a timeout causes is the timeout's to answer and never a 500.
app.UseRequestTimeouts();
// Before authentication, so a preflight (which carries no token) is answered and a 401 still carries the CORS headers.
app.UseCors();
app.UseAuthentication();
app.UseAuthorization();
app.UseKeyRateLimits();

app.MapGet("/health", () => new HealthResponse("ok", ServerVersion.Current));
// The OpenAPI document describes the API and holds nothing private, so like /health it takes no token.
app.MapOpenApi(OpenApiSetup.Path);
app.MapProjectEndpoints();
app.MapEnvironmentEndpoints();
app.MapFlagEndpoints();
app.MapApiKeyEndpoints();
app.MapAdminTokenEndpoints();
app.MapConfigEndpoints();
app.MapFlagStateEndpoints();
app.MapSegmentEndpoints();
app.MapAuditEndpoints();
app.MapSdkEndpoints();
app.MapDiagnoseEndpoint();
app.MapRuntimeEndpoint();
app.MapUsageEndpoints();

app.Run();

public sealed record HealthResponse(string Status, string Version);

public static class ServerVersion
{
    /// <summary>The project's <c>Version</c> (the SDK's source-revision suffix is switched off in the csproj).</summary>
    public static string Current { get; } = typeof(ServerVersion).Assembly
        .GetCustomAttribute<AssemblyInformationalVersionAttribute>()!.InformationalVersion;
}
