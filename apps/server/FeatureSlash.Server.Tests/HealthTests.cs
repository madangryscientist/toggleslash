using System.Net;
using System.Text.Json;

public sealed class HealthTests(TempDatabaseServer factory) : IClassFixture<TempDatabaseServer>
{
    [Fact]
    public async Task Health_returns_ok_and_the_version()
    {
        using var client = factory.CreateClient();

        var response = await client.GetAsync("/health");

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Equal("application/json; charset=utf-8", response.Content.Headers.ContentType?.ToString());
        using var body = JsonDocument.Parse(
            await response.Content.ReadAsStringAsync());
        Assert.Equal("ok", body.RootElement.GetProperty("status").GetString());
        Assert.Equal("0.0.0", body.RootElement.GetProperty("version").GetString());
    }

    [Fact]
    public async Task Unknown_routes_are_404()
    {
        using var client = factory.CreateClient();

        var response = await client.GetAsync("/nope");

        Assert.Equal(HttpStatusCode.NotFound, response.StatusCode);
    }
}
