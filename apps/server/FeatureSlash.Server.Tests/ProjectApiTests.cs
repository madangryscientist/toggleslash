using System.Net;

public sealed class ProjectApiTests : IDisposable
{
    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    [Fact]
    public async Task Create_then_read_and_list()
    {
        var created = await server.Post("/api/v1/projects", new { key = "shop", name = "  Shop  " });

        await ApiServer.Expect(HttpStatusCode.Created, created);
        Assert.Equal("/api/v1/projects/shop", created.Headers.Location!.OriginalString);
        var body = await ApiServer.Json(created);
        Assert.Equal("shop", body.GetProperty("key").GetString());
        Assert.Equal("Shop", body.GetProperty("name").GetString());
        Assert.True(body.GetProperty("createdAt").GetDateTime() > DateTime.UtcNow.AddMinutes(-1));

        await server.SeedProject("blog");
        var read = await ApiServer.Json(await server.Get("/api/v1/projects/shop"));
        Assert.Equal(body.GetRawText(), read.GetRawText());
        var list = await ApiServer.Json(await server.Get("/api/v1/projects"));
        Assert.Equal(["shop", "blog"], list.EnumerateArray().Select(p => p.GetProperty("key").GetString()));
    }

    [Fact]
    public async Task An_empty_list_is_an_empty_array()
    {
        var list = await ApiServer.Json(await server.Get("/api/v1/projects"));

        Assert.Equal(0, list.GetArrayLength());
    }

    [Theory]
    [InlineData(null, "Shop")]
    [InlineData("", "Shop")]
    [InlineData("Shop", "Shop")]
    [InlineData("-shop", "Shop")]
    [InlineData("shop key", "Shop")]
    [InlineData("shop", null)]
    [InlineData("shop", "   ")]
    public async Task A_bad_key_or_name_is_400(string? key, string? name)
    {
        var response = await server.Post("/api/v1/projects", new { key, name });

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", response);
        Assert.Equal(0, (await ApiServer.Json(await server.Get("/api/v1/projects"))).GetArrayLength());
    }

    [Fact]
    public async Task A_key_is_at_most_64_characters()
    {
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects", new { key = new string('a', 64), name = "ok" }));
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await server.Post("/api/v1/projects", new { key = new string('a', 65), name = "too long" }));
    }

    [Fact]
    public async Task An_unreadable_body_is_400()
    {
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Post, "/api/v1/projects")
        {
            Content = new StringContent("{not json", System.Text.Encoding.UTF8, "application/json"),
        };
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {ApiServer.AdminToken}");

        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await client.SendAsync(request));
    }

    [Fact]
    public async Task A_taken_key_is_409()
    {
        await server.SeedProject("shop");

        var response = await server.Post("/api/v1/projects", new { key = "shop", name = "Another" });

        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", response);
    }

    [Fact]
    public async Task An_unknown_project_is_404()
    {
        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Get("/api/v1/projects/nope"));
    }
}
