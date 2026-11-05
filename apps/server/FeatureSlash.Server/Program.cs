using System.Reflection;

var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();

app.MapGet("/health", () => new HealthResponse("ok", ServerVersion.Current));

app.Run();

public sealed record HealthResponse(string Status, string Version);

public static class ServerVersion
{
    /// <summary>The project's <c>Version</c> (the SDK's source-revision suffix is switched off in the csproj).</summary>
    public static string Current { get; } = typeof(ServerVersion).Assembly
        .GetCustomAttribute<AssemblyInformationalVersionAttribute>()!.InformationalVersion;
}
