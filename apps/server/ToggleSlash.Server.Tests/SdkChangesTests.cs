using System.Net;
using System.Threading.Channels;
using ToggleSlash.Server.Sdk;
using Microsoft.AspNetCore.TestHost;
using Microsoft.Extensions.DependencyInjection;

/// <summary>
/// <c>GET /sdk/v1/changes</c>, the change signal a client key may open: the stream's events with <c>{}</c> for data.
/// The heartbeat is ticked by hand, and <see cref="Guard"/> only bounds a wait so a broken stream fails instead of hanging.
/// </summary>
public sealed class SdkChangesTests : IDisposable
{
    private const string ChangesPath = "/sdk/v1/changes";
    private const string StreamPath = "/sdk/v1/stream";
    private const string Ruleset = "/sdk/v1/ruleset";
    private const string Flags = "/api/v1/projects/shop/flags";
    private const string Origin = "https://app.example";

    private static readonly TimeSpan Guard = TimeSpan.FromSeconds(10);

    private readonly ManualHeartbeat heartbeat = new();
    private readonly ApiServer server;

    public SdkChangesTests() =>
        server = ApiServer.With(builder => builder.ConfigureTestServices(services => services.AddSingleton<StreamHeartbeat>(heartbeat)));

    public void Dispose() => server.Dispose();

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedFlag("shop", "dark-mode");
    }

    private async Task<string> Key(string kind)
    {
        var created = await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private async Task<string> ETagOf(string serverKey)
    {
        var response = await server.Send(HttpMethod.Get, Ruleset, $"Bearer {serverKey}");
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return response.Headers.ETag!.ToString();
    }

    private async Task Toggle(bool on) =>
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flags}/dark-mode/environments/prod/toggle", new { on }));

    private Task<OpenStream> Open(string key) => OpenStream.Connect(server, key);

    // --- who may open it ---------------------------------------------------------------------------------------

    [Fact]
    public async Task A_client_key_connects_and_hears_connected_with_nothing_in_it()
    {
        await Seed();
        using var stream = await Open(await Key("client"));

        Assert.Equal("text/event-stream", stream.Response.Content.Headers.ContentType!.MediaType);
        Assert.True(stream.Response.Headers.CacheControl!.NoCache);
        Assert.Equal(("connected", "{}"), await stream.NextEvent());
    }

    [Fact]
    public async Task A_server_key_gets_a_403_and_no_key_a_401()
    {
        await Seed();
        var serverKey = await Key("server");

        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Get, ChangesPath, $"Bearer {serverKey}"));
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await server.Send(HttpMethod.Get, ChangesPath, null));
    }

    [Fact]
    public async Task A_client_key_still_cannot_read_the_stream_or_the_ruleset()
    {
        await Seed();
        var client = await Key("client");

        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Get, StreamPath, $"Bearer {client}"));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Get, Ruleset, $"Bearer {client}"));
    }

    // --- a write -----------------------------------------------------------------------------------------------

    [Fact]
    public async Task A_write_sends_changed_with_nothing_in_it_and_a_no_op_write_sends_nothing()
    {
        await Seed();
        await server.SeedFlag("shop", "retired");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flags}/retired/archive"));
        var serverKey = await Key("server");
        var before = await ETagOf(serverKey);
        using var stream = await Open(await Key("client"));
        await stream.NextEvent();

        // Archiving an archived flag succeeds and signals; the served bytes are the same, so nothing is sent.
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flags}/retired/archive"));
        await Toggle(on: true);

        Assert.Equal(("changed", "{}"), await stream.NextEvent());
        heartbeat.Tick();
        await stream.NextKeepAlive();

        // The signal carried nothing of the ruleset: not the tag it had, nor the one it has now.
        var after = await ETagOf(serverKey);
        Assert.NotEqual(before, after);
        foreach (var etag in new[] { before, after })
        {
            Assert.DoesNotContain(etag, stream.Transcript);
            Assert.DoesNotContain(etag.Trim('"'), stream.Transcript);
        }
        Assert.DoesNotContain("dark-mode", stream.Transcript);
    }

    // --- the end of a stream -----------------------------------------------------------------------------------

    [Fact]
    public async Task A_key_that_is_revoked_ends_the_stream()
    {
        await Seed();
        var created = await ApiServer.Json(await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind = "client", name = "revocable" }));
        using var stream = await Open(created.GetProperty("key").GetString()!);
        await stream.NextEvent();
        heartbeat.Tick();
        await stream.NextKeepAlive();

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"/api/v1/projects/shop/environments/prod/keys/{created.GetProperty("id").GetInt64()}"));
        heartbeat.Tick();

        await stream.Ended();
    }

    // --- CORS --------------------------------------------------------------------------------------------------

    [Fact]
    public async Task A_preflight_for_the_change_signal_is_allowed_from_any_origin_with_authorization()
    {
        var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Options, ChangesPath);
        request.Headers.TryAddWithoutValidation("Origin", Origin);
        request.Headers.TryAddWithoutValidation("Access-Control-Request-Method", "GET");
        request.Headers.TryAddWithoutValidation("Access-Control-Request-Headers", "authorization");

        var response = await client.SendAsync(request);

        Assert.True(response.IsSuccessStatusCode, response.StatusCode.ToString());
        Assert.Equal(["*"], response.Headers.GetValues("Access-Control-Allow-Origin"));
        Assert.Contains("GET", string.Join(',', response.Headers.GetValues("Access-Control-Allow-Methods")));
        Assert.Contains("Authorization", string.Join(',', response.Headers.GetValues("Access-Control-Allow-Headers")), StringComparison.OrdinalIgnoreCase);
    }

    [Theory]
    [InlineData(StreamPath)]
    [InlineData(Ruleset)]
    public async Task The_stream_and_the_ruleset_still_answer_no_cors_headers(string path)
    {
        await Seed();
        var client = server.Server.CreateClient();
        using var preflight = new HttpRequestMessage(HttpMethod.Options, path);
        preflight.Headers.TryAddWithoutValidation("Origin", Origin);
        preflight.Headers.TryAddWithoutValidation("Access-Control-Request-Method", "GET");
        preflight.Headers.TryAddWithoutValidation("Access-Control-Request-Headers", "authorization");
        using var read = new HttpRequestMessage(HttpMethod.Get, path);
        read.Headers.TryAddWithoutValidation("Origin", Origin);
        read.Headers.TryAddWithoutValidation("Authorization", $"Bearer {await Key("client")}");

        var answered = await client.SendAsync(preflight);
        var refused = await client.SendAsync(read);

        Assert.False(answered.Headers.Contains("Access-Control-Allow-Origin"));
        Assert.False(refused.Headers.Contains("Access-Control-Allow-Origin"));
    }

    // --- the harness -------------------------------------------------------------------------------------------

    /// <summary>A client reading the signal line by line, keeping everything it read.</summary>
    private sealed class OpenStream(HttpClient client, HttpResponseMessage response, StreamReader reader) : IDisposable
    {
        private readonly List<string> lines = [];

        public HttpResponseMessage Response => response;

        /// <summary>Every line read so far.</summary>
        public string Transcript => string.Join('\n', lines);

        public static async Task<OpenStream> Connect(ApiServer server, string key)
        {
            var client = server.Server.CreateClient();
            using var request = new HttpRequestMessage(HttpMethod.Get, ChangesPath);
            request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {key}");
            var response = await client.SendAsync(request, HttpCompletionOption.ResponseHeadersRead).WaitAsync(Guard);
            Assert.Equal(HttpStatusCode.OK, response.StatusCode);
            return new OpenStream(client, response, new StreamReader(await response.Content.ReadAsStreamAsync()));
        }

        public async Task<string?> NextLine()
        {
            var line = await reader.ReadLineAsync().WaitAsync(Guard);
            if (line is not null)
            {
                lines.Add(line);
            }
            return line;
        }

        public async Task<(string Name, string Data)> NextEvent()
        {
            var name = await NextLine();
            var data = await NextLine();
            Assert.StartsWith("event: ", name);
            Assert.StartsWith("data: ", data);
            Assert.Equal("", await NextLine());
            return (name!["event: ".Length..], data!["data: ".Length..]);
        }

        public async Task NextKeepAlive()
        {
            Assert.Equal(": keep-alive", await NextLine());
            Assert.Equal("", await NextLine());
        }

        public async Task Ended() => Assert.Null(await NextLine());

        public void Dispose()
        {
            response.Dispose();
            client.Dispose();
        }
    }

    /// <summary>A heartbeat that is due when the test says so.</summary>
    private sealed class ManualHeartbeat : StreamHeartbeat
    {
        private readonly Channel<bool> ticks = Channel.CreateUnbounded<bool>();

        public void Tick() => ticks.Writer.TryWrite(true);

        public override Task Next(CancellationToken cancellation) => ticks.Reader.ReadAsync(cancellation).AsTask();
    }
}
