using System.Reflection;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Data;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddServerDatabase();
builder.Services.AddServerAuth();
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
app.UseAuthentication();
app.UseAuthorization();

app.MapGet("/health", () => new HealthResponse("ok", ServerVersion.Current));
app.MapApiKeyEndpoints();

app.Run();

public sealed record HealthResponse(string Status, string Version);

public static class ServerVersion
{
    /// <summary>The project's <c>Version</c> (the SDK's source-revision suffix is switched off in the csproj).</summary>
    public static string Current { get; } = typeof(ServerVersion).Assembly
        .GetCustomAttribute<AssemblyInformationalVersionAttribute>()!.InformationalVersion;
}
