using System.Reflection;
using System.Text.Json.Serialization;
using FeatureSlash.Server.Audit;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Catalogue;
using FeatureSlash.Server.Data;
using FeatureSlash.Server.Sdk;
using FeatureSlash.Server.Targeting;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddServerDatabase();
builder.Services.AddServerAuth();
builder.Services.AddServerOpenApi();
builder.Services.AddSdkApi();
builder.Services.AddSingleton<TargetingLock>();
// A number is a JSON number: "1" for an index is a 400, and the OpenAPI document says integer, not "integer or string".
builder.Services.ConfigureHttpJsonOptions(options => options.SerializerOptions.NumberHandling = JsonNumberHandling.Strict);
// An unreadable body is the client's 400 in every environment, not a Development-only exception.
builder.Services.Configure<RouteHandlerOptions>(options => options.ThrowOnBadRequest = false);
var app = builder.Build();

app.Services.MigrateServerDatabase();

// Explicit, in this order: left to the host, authentication would run outside the error handling.
app.UseExceptionHandler(new ExceptionHandlerOptions
{
    ExceptionHandler = context =>
        ApiError.Write(context, StatusCodes.Status500InternalServerError, "internal_server_error", "Internal Server Error"),
});
app.UseStatusCodePages(context => ApiError.WriteForStatus(context.HttpContext));
// Before authentication, so a preflight (which carries no token) is answered and a 401 still carries the CORS headers.
app.UseCors();
app.UseAuthentication();
app.UseAuthorization();

app.MapGet("/health", () => new HealthResponse("ok", ServerVersion.Current));
// The OpenAPI document describes the API and holds nothing private, so like /health it takes no token.
app.MapOpenApi(OpenApiSetup.Path);
app.MapProjectEndpoints();
app.MapEnvironmentEndpoints();
app.MapFlagEndpoints();
app.MapApiKeyEndpoints();
app.MapConfigEndpoints();
app.MapSegmentEndpoints();
app.MapAuditEndpoints();
app.MapSdkEndpoints();

app.Run();

public sealed record HealthResponse(string Status, string Version);

public static class ServerVersion
{
    /// <summary>The project's <c>Version</c> (the SDK's source-revision suffix is switched off in the csproj).</summary>
    public static string Current { get; } = typeof(ServerVersion).Assembly
        .GetCustomAttribute<AssemblyInformationalVersionAttribute>()!.InformationalVersion;
}
