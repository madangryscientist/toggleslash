using System.Net;

public sealed class EnvironmentApiTests : IDisposable
{
    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    [Fact]
    public async Task Create_then_read_and_list()
    {
        await server.SeedProject();

        var created = await server.Post("/api/v1/projects/shop/environments", new { key = "prod", name = " Production " });

        await ApiServer.Expect(HttpStatusCode.Created, created);
        Assert.Equal("/api/v1/projects/shop/environments/prod", created.Headers.Location!.OriginalString);
        var body = await ApiServer.Json(created);
        Assert.Equal("Production", body.GetProperty("name").GetString());

        await server.SeedEnvironment("shop", "staging");
        var read = await ApiServer.Json(await server.Get("/api/v1/projects/shop/environments/prod"));
        Assert.Equal(body.GetRawText(), read.GetRawText());
        var list = await ApiServer.Json(await server.Get("/api/v1/projects/shop/environments"));
        Assert.Equal(["prod", "staging"], list.EnumerateArray().Select(e => e.GetProperty("key").GetString()));
    }

    [Fact]
    public async Task The_same_key_in_two_projects_is_fine_and_lists_stay_apart()
    {
        await server.SeedProject("shop");
        await server.SeedProject("blog");
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("blog", "prod");

        var list = await ApiServer.Json(await server.Get("/api/v1/projects/blog/environments"));

        Assert.Equal(1, list.GetArrayLength());
    }

    [Theory]
    [InlineData(null, "Prod")]
    [InlineData("Prod", "Prod")]
    [InlineData("_prod", "Prod")]
    [InlineData("prod", null)]
    [InlineData("prod", "")]
    public async Task A_bad_key_or_name_is_400(string? key, string? name)
    {
        await server.SeedProject();

        var response = await server.Post("/api/v1/projects/shop/environments", new { key, name });

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", response);
    }

    [Fact]
    public async Task A_taken_key_is_409()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");

        var response = await server.Post("/api/v1/projects/shop/environments", new { key = "prod", name = "Again" });

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", response);
    }

    [Fact]
    public async Task An_unknown_project_or_environment_is_404()
    {
        await server.SeedProject();

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Post("/api/v1/projects/nope/environments", new { key = "prod", name = "Prod" }));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/nope/environments"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/nope/environments/prod"));
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/shop/environments/prod"));
    }

    [Fact]
    public async Task A_validation_failure_beats_a_missing_project()
    {
        // The request is judged before the project is looked up, so a bad key says so, not "no such project".
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await server.Post("/api/v1/projects/nope/environments", new { key = "Bad Key", name = "x" }));
    }

    [Fact]
    public async Task A_new_environment_gets_an_off_config_for_every_existing_flag_archived_ones_too()
    {
        await server.SeedProject();
        await server.SeedFlag("shop", "dark-mode");
        await server.SeedFlag("shop", "old-flag");
        await server.Post("/api/v1/projects/shop/flags/old-flag/archive");
        await server.SeedProject("blog");
        await server.SeedFlag("blog", "elsewhere");

        await server.SeedEnvironment("shop", "prod");

        var darkMode = Assert.Single(server.Configs("dark-mode"));
        Assert.Equal(("prod", false, 1L, """{"variation":0}"""), darkMode);
        Assert.Single(server.Configs("old-flag"));
        Assert.Empty(server.Configs("elsewhere"));
    }
}
