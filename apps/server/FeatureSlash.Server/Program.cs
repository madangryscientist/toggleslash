using System.Reflection;
using FeatureSlash.Server.Data;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddServerDatabase();
var app = builder.Build();

app.Services.MigrateServerDatabase();

app.MapGet("/health", () => new HealthResponse("ok", ServerVersion.Current));

app.Run();

public sealed record HealthResponse(string Status, string Version);

public static class ServerVersion
{
    /// <summary>The project's <c>Version</c> (the SDK's source-revision suffix is switched off in the csproj).</summary>
    public static string Current { get; } = typeof(ServerVersion).Assembly
        .GetCustomAttribute<AssemblyInformationalVersionAttribute>()!.InformationalVersion;
}
