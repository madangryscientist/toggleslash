using System.Net;
using System.Text.Json;
using System.Text.Json.Nodes;

public sealed class OpenApiTests : IDisposable
{
    private const string UpdateVariable = "FEATURESLASH_UPDATE_OPENAPI";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private async Task<JsonNode> Served()
    {
        var response = await server.Send(HttpMethod.Get, "/openapi/v1.json", null);
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return JsonNode.Parse(await response.Content.ReadAsStringAsync())!;
    }

    /// <summary>The repo's <c>apps/server/openapi/v1.json</c>, found from the test binary by the <c>global.json</c> at the root.</summary>
    private static string CommittedPath()
    {
        var directory = new DirectoryInfo(AppContext.BaseDirectory);
        while (directory is not null && !File.Exists(Path.Combine(directory.FullName, "global.json")))
        {
            directory = directory.Parent;
        }
        Assert.NotNull(directory);
        return Path.Combine(directory.FullName, "apps", "server", "openapi", "v1.json");
    }

    [Fact]
    public async Task The_committed_document_is_what_the_server_serves()
    {
        var served = await Served();
        var path = CommittedPath();

        if (Environment.GetEnvironmentVariable(UpdateVariable) == "1")
        {
            Directory.CreateDirectory(Path.GetDirectoryName(path)!);
            File.WriteAllText(path, served.ToJsonString(new JsonSerializerOptions { WriteIndented = true }) + "\n");
        }

        Assert.True(File.Exists(path), $"{path} is missing. Generate it: {UpdateVariable}=1 dotnet test apps/server/FeatureSlash.Server.Tests");
        var committed = JsonNode.Parse(File.ReadAllText(path))!;
        Assert.True(
            JsonNode.DeepEquals(committed, served),
            $"apps/server/openapi/v1.json is out of date with the API. Regenerate it: {UpdateVariable}=1 dotnet test apps/server/FeatureSlash.Server.Tests");
    }

    [Fact]
    public async Task The_document_needs_no_token_and_does_not_depend_on_the_caller()
    {
        var anonymous = await Served();
        var withToken = await server.AsAdmin(HttpMethod.Get, "/openapi/v1.json");

        Assert.True(JsonNode.DeepEquals(anonymous, JsonNode.Parse(await withToken.Content.ReadAsStringAsync())));
        Assert.Null(anonymous["servers"]);
    }

    [Fact]
    public async Task Every_catalogue_operation_is_documented_with_the_admin_token()
    {
        var paths = (await Served())["paths"]!.AsObject();

        foreach (var (path, method) in new[]
        {
            ("/api/v1/projects", "post"), ("/api/v1/projects", "get"), ("/api/v1/projects/{project}", "get"),
            ("/api/v1/projects/{project}/environments", "post"), ("/api/v1/projects/{project}/environments", "get"),
            ("/api/v1/projects/{project}/environments/{env}", "get"),
            ("/api/v1/projects/{project}/flags", "post"), ("/api/v1/projects/{project}/flags", "get"),
            ("/api/v1/projects/{project}/flags/{flag}", "get"), ("/api/v1/projects/{project}/flags/{flag}", "patch"),
            ("/api/v1/projects/{project}/flags/{flag}/archive", "post"), ("/api/v1/projects/{project}/flags/{flag}/restore", "post"),
        })
        {
            var operation = paths[path]?[method];
            Assert.True(operation is not null, $"{method.ToUpperInvariant()} {path} is not in the document");
            Assert.NotNull(operation["operationId"]);
            Assert.NotNull(operation["security"]);
        }
    }
}
