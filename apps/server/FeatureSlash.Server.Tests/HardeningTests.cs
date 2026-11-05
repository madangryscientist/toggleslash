using System.Data.Common;
using System.Net;
using System.Net.Http.Headers;
using System.Text;
using FeatureSlash.Server.Hardening;
using FeatureSlash.Server.Limits;
using FeatureSlash.Server.Sdk;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http.Timeouts;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using Microsoft.AspNetCore.TestHost;
using Microsoft.EntityFrameworkCore;
using Microsoft.EntityFrameworkCore.Diagnostics;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Options;

/// <summary>
/// The response headers, the body limits and the timeouts. A test that needs Kestrel itself (its limits are not
/// TestServer's) starts it with <c>UseKestrel</c> on a loopback port. Nothing here waits for a real 30 seconds.
/// </summary>
public sealed class HardeningTests : IDisposable
{
    private const string Ruleset = "/sdk/v1/ruleset";
    private const string Stream = "/sdk/v1/stream";
    private const string Evaluate = "/sdk/v1/evaluate";
    private const string Events = "/sdk/v1/events";
    private const string Projects = "/api/v1/projects";
    private const string Origin = "https://app.example";

    private static readonly TimeSpan Guard = TimeSpan.FromSeconds(10);

    private readonly List<IDisposable> started = [];
    private readonly Slowness slowness = new();

    public void Dispose()
    {
        foreach (var server in started)
        {
            server.Dispose();
        }
    }

    private ApiServer Start(Action<IServiceCollection>? services = null, params (string Setting, string Value)[] settings)
    {
        var server = ApiServer.With(builder =>
        {
            foreach (var (setting, value) in settings)
            {
                builder.UseSetting(setting, value);
            }
            builder.ConfigureTestServices(collection =>
            {
                collection.ConfigureDbContext<FeatureSlash.Server.Data.FeatureSlashDbContext>(options => options.AddInterceptors(slowness));
                services?.Invoke(collection);
            });
        });
        started.Add(server);
        return server;
    }

    private static async Task<string> Key(ApiServer server, string kind)
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        var created = await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private static async Task<HttpResponseMessage> Call(ApiServer server, HttpMethod method, string path, string? key = null, HttpContent? content = null, string? origin = null)
    {
        var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(method, path) { Content = content };
        if (key is not null)
        {
            request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {key}");
        }
        if (origin is not null)
        {
            request.Headers.TryAddWithoutValidation("Origin", origin);
        }
        return await client.SendAsync(request, HttpCompletionOption.ResponseHeadersRead).WaitAsync(Guard);
    }

    private static StringContent Json(string text) => new(text, Encoding.UTF8, "application/json");

    private static void ExpectHeaders(HttpResponseMessage response)
    {
        Assert.Equal(["nosniff"], response.Headers.GetValues("X-Content-Type-Options"));
        Assert.Equal(["no-referrer"], response.Headers.GetValues("Referrer-Policy"));
        Assert.Equal(["DENY"], response.Headers.GetValues("X-Frame-Options"));
        // The server does not terminate TLS, so it never says Strict-Transport-Security.
        Assert.False(response.Headers.Contains("Strict-Transport-Security"));
    }

    // --- the headers on every kind of answer ----------------------------------------------------------------

    [Fact]
    public async Task A_200_carries_the_three_headers()
    {
        var response = await Call(Start(), HttpMethod.Get, "/health");

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        ExpectHeaders(response);
    }

    [Fact]
    public async Task A_401_carries_the_three_headers()
    {
        var response = await Call(Start(), HttpMethod.Get, Projects);

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", response);
        ExpectHeaders(response);
    }

    [Fact]
    public async Task A_404_carries_the_three_headers()
    {
        var response = await Call(Start(), HttpMethod.Get, "/nothing-here");

        Assert.Equal(HttpStatusCode.NotFound, response.StatusCode);
        ExpectHeaders(response);
    }

    [Fact]
    public async Task A_413_carries_the_three_headers()
    {
        var server = Start();
        var key = await Key(server, "client");

        var response = await Call(server, HttpMethod.Post, Events, key, Json($$"""{"events":[],"padding":"{{new string('x', 128 * 1024)}}"}"""));

        await ApiServer.ExpectError(HttpStatusCode.RequestEntityTooLarge, "payload_too_large", response);
        ExpectHeaders(response);
    }

    [Fact]
    public async Task A_429_carries_the_three_headers()
    {
        var server = Start(null, (RateLimitSettings.EvaluateSetting, "1"));
        var key = await Key(server, "client");
        var body = """{"context":{"key":"u1"}}""";
        await ApiServer.Expect(HttpStatusCode.OK, await Call(server, HttpMethod.Post, Evaluate, key, Json(body)));

        var response = await Call(server, HttpMethod.Post, Evaluate, key, Json(body));

        await ApiServer.ExpectError(HttpStatusCode.TooManyRequests, "rate_limited", response);
        ExpectHeaders(response);
    }

    [Fact]
    public async Task A_500_carries_the_three_headers()
    {
        var server = Start();
        var key = await Key(server, "server");
        slowness.Mode = Mode.Fail;

        var response = await Call(server, HttpMethod.Get, Ruleset, key);

        await ApiServer.ExpectError(HttpStatusCode.InternalServerError, "internal_server_error", response);
        ExpectHeaders(response);
    }

    [Fact]
    public async Task The_stream_carries_the_three_headers_and_keeps_its_own_cache_control()
    {
        var server = Start();
        var key = await Key(server, "server");

        using var response = await Call(server, HttpMethod.Get, Stream, key);

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        ExpectHeaders(response);
        Assert.True(response.Headers.CacheControl!.NoCache);
        Assert.False(response.Headers.CacheControl.NoStore);
    }

    [Theory]
    [InlineData("GET", "/api/v1/projects")]
    [InlineData("GET", "/api/v1/nothing")]
    public async Task Admin_responses_are_no_store(string method, string path)
    {
        var server = Start();
        await Key(server, "server");

        var response = await server.Send(new HttpMethod(method), path, $"Bearer {ApiServer.AdminToken}");

        Assert.True(response.Headers.CacheControl!.NoStore, $"{method} {path}: {response.Headers.CacheControl}");
    }

    [Fact]
    public async Task A_401_on_an_admin_route_is_no_store_and_an_admin_write_is_too()
    {
        var server = Start();

        Assert.True((await Call(server, HttpMethod.Get, Projects)).Headers.CacheControl!.NoStore);
        var created = await server.Post(Projects, new { key = "shop", name = "shop" });
        Assert.Equal(HttpStatusCode.Created, created.StatusCode);
        Assert.True(created.Headers.CacheControl!.NoStore);
    }

    [Fact]
    public async Task The_ruleset_keeps_its_etag_its_304_and_its_own_cache_control()
    {
        var server = Start();
        var key = await Key(server, "server");

        var first = await Call(server, HttpMethod.Get, Ruleset, key);
        var etag = first.Headers.ETag!;
        var client = server.Server.CreateClient();
        using var again = new HttpRequestMessage(HttpMethod.Get, Ruleset);
        again.Headers.TryAddWithoutValidation("Authorization", $"Bearer {key}");
        again.Headers.IfNoneMatch.Add(etag);
        var revalidated = await client.SendAsync(again);

        Assert.Equal(HttpStatusCode.OK, first.StatusCode);
        Assert.True(first.Headers.CacheControl!.NoCache && first.Headers.CacheControl.Private && !first.Headers.CacheControl.NoStore);
        Assert.Equal(HttpStatusCode.NotModified, revalidated.StatusCode);
        Assert.Equal(etag, revalidated.Headers.ETag);
        Assert.True(revalidated.Headers.CacheControl!.NoCache && revalidated.Headers.CacheControl.Private && !revalidated.Headers.CacheControl.NoStore);
        ExpectHeaders(first);
        ExpectHeaders(revalidated);
    }

    // --- CORS survives the headers --------------------------------------------------------------------------

    [Theory]
    [InlineData(Evaluate)]
    [InlineData(Events)]
    public async Task A_preflight_still_answers_with_its_cors_headers(string path)
    {
        var client = Start().Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Options, path);
        request.Headers.TryAddWithoutValidation("Origin", Origin);
        request.Headers.TryAddWithoutValidation("Access-Control-Request-Method", "POST");
        request.Headers.TryAddWithoutValidation("Access-Control-Request-Headers", "authorization,content-type");

        var response = await client.SendAsync(request);

        Assert.True(response.IsSuccessStatusCode, response.StatusCode.ToString());
        Assert.Equal(["*"], response.Headers.GetValues("Access-Control-Allow-Origin"));
        Assert.Contains("POST", string.Join(',', response.Headers.GetValues("Access-Control-Allow-Methods")));
        Assert.Equal(["600"], response.Headers.GetValues("Access-Control-Max-Age"));
        ExpectHeaders(response);
    }

    [Fact]
    public async Task A_cors_response_keeps_its_cors_headers_next_to_ours_and_a_500_does_too()
    {
        var server = Start();
        var key = await Key(server, "client");

        var ok = await Call(server, HttpMethod.Post, Evaluate, key, Json("""{"context":{"key":"u1"}}"""), Origin);
        Assert.Equal(["*"], ok.Headers.GetValues("Access-Control-Allow-Origin"));
        ExpectHeaders(ok);

        slowness.Mode = Mode.Fail;
        var failed = await Call(server, HttpMethod.Post, Evaluate, key, Json("""{"context":{"key":"u1"}}"""), Origin);
        Assert.Equal(HttpStatusCode.InternalServerError, failed.StatusCode);
        Assert.Equal(["*"], failed.Headers.GetValues("Access-Control-Allow-Origin"));
        ExpectHeaders(failed);
    }

    // --- body limits ----------------------------------------------------------------------------------------

    [Fact]
    public void Kestrel_is_set_to_1_MiB_a_body_and_the_documented_timeouts()
    {
        var limits = Start().Server.Services.GetRequiredService<IOptions<KestrelServerOptions>>().Value.Limits;

        Assert.Equal(1024 * 1024, limits.MaxRequestBodySize);
        Assert.Equal(TimeSpan.FromSeconds(30), limits.RequestHeadersTimeout);
        Assert.Equal(TimeSpan.FromMinutes(2), limits.KeepAliveTimeout);
        // Left at the framework's default on purpose: 240 bytes a second after a 5 second grace.
        Assert.Equal(240, limits.MinRequestBodyDataRate!.BytesPerSecond);
        Assert.Equal(TimeSpan.FromSeconds(5), limits.MinRequestBodyDataRate.GracePeriod);
    }

    private ApiServer OnKestrel()
    {
        var server = Start();
        server.Server.UseKestrel();
        return server;
    }

    [Fact]
    public async Task A_body_over_1_MiB_on_an_ordinary_post_is_a_413_in_the_error_shape()
    {
        var server = OnKestrel();

        var response = await server.Post(Projects, new { key = "shop", name = new string('x', 1024 * 1024) });

        await ApiServer.ExpectError(HttpStatusCode.RequestEntityTooLarge, "payload_too_large", response);
        ExpectHeaders(response);
        Assert.True(response.Headers.CacheControl!.NoStore);
    }

    [Fact]
    public async Task A_chunked_body_over_1_MiB_is_a_413_too_when_the_read_is_what_fails()
    {
        var server = OnKestrel();
        // No Content-Length: Kestrel only finds out as the bytes arrive.
        var content = new StreamContent(new RepeatingStream(1024 * 1024 + 1));
        content.Headers.ContentType = new MediaTypeHeaderValue("application/json");
        Assert.Null(content.Headers.ContentLength);

        var response = await Call(server, HttpMethod.Post, Projects, ApiServer.AdminToken, content);

        await ApiServer.ExpectError(HttpStatusCode.RequestEntityTooLarge, "payload_too_large", response);
        ExpectHeaders(response);
    }

    [Fact]
    public async Task A_body_just_under_the_limit_is_still_read()
    {
        var server = OnKestrel();

        var response = await server.Post(Projects, new { key = "shop", name = new string('x', 1024 * 1024 - 100) });

        // Past the name's own length rule, which is the route's to refuse: the limit did not.
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", response);
    }

    // --- timeouts -------------------------------------------------------------------------------------------

    /// <summary>The default policy, shortened. The middleware reads it once at startup, so it cannot be changed later.</summary>
    private static Action<IServiceCollection> TimeoutOf(TimeSpan timeout, Action<IServiceCollection>? more = null) =>
        services =>
        {
            services.Configure<RequestTimeoutOptions>(options => options.DefaultPolicy = RequestLimits.TimeoutPolicy(timeout));
            more?.Invoke(services);
        };

    [Fact]
    public async Task A_request_past_its_timeout_is_answered_in_the_error_shape()
    {
        // Long enough that seeding the key, cold, is not what runs into it.
        var server = Start(TimeoutOf(TimeSpan.FromSeconds(1)));
        var key = await Key(server, "server");
        slowness.Mode = Mode.Hang;

        var response = await Call(server, HttpMethod.Get, Ruleset, key);

        await ApiServer.ExpectError(HttpStatusCode.GatewayTimeout, "request_timeout", response);
        ExpectHeaders(response);
    }

    [Fact]
    public async Task A_stream_stays_open_past_that_same_timeout_and_its_heartbeat_still_works()
    {
        var heartbeat = new TickedHeartbeat();
        var server = Start(TimeoutOf(TimeSpan.FromMilliseconds(500), services => services.AddSingleton<StreamHeartbeat>(heartbeat)));
        var key = await Key(server, "server");

        using var response = await Call(server, HttpMethod.Get, Stream, key);
        using var reader = new StreamReader(await response.Content.ReadAsStreamAsync());
        Assert.Equal("event: connected", await reader.ReadLineAsync().WaitAsync(Guard));
        await reader.ReadLineAsync().WaitAsync(Guard);
        await reader.ReadLineAsync().WaitAsync(Guard);

        // Twice the timeout. A route that was still under it would have been cut off by now.
        await Task.Delay(1000);
        heartbeat.Tick();

        Assert.Equal(": keep-alive", await reader.ReadLineAsync().WaitAsync(Guard));
    }

    // --- test seams -----------------------------------------------------------------------------------------

    private enum Mode
    {
        Normal,

        /// <summary>A database read that never returns on its own: only the request's token ends it.</summary>
        Hang,
        Fail,
    }

    private sealed class Slowness : DbCommandInterceptor
    {
        public volatile Mode Mode;

        public override async ValueTask<InterceptionResult<DbDataReader>> ReaderExecutingAsync(
            DbCommand command, CommandEventData eventData, InterceptionResult<DbDataReader> result, CancellationToken cancellationToken)
        {
            switch (Mode)
            {
                case Mode.Hang:
                    await Task.Delay(Timeout.Infinite, cancellationToken);
                    break;
                case Mode.Fail:
                    throw new InvalidOperationException("the database is down");
            }
            return result;
        }
    }

    private sealed class TickedHeartbeat : StreamHeartbeat
    {
        private readonly System.Threading.Channels.Channel<bool> ticks = System.Threading.Channels.Channel.CreateUnbounded<bool>();

        public void Tick() => ticks.Writer.TryWrite(true);

        public override Task Next(CancellationToken cancellation) => ticks.Reader.ReadAsync(cancellation).AsTask();
    }

    /// <summary>A JSON-looking body of a given length with no known length, so it goes out chunked.</summary>
    private sealed class RepeatingStream(long length) : Stream
    {
        private long sent;

        public override bool CanRead => true;
        public override bool CanSeek => false;
        public override bool CanWrite => false;
        public override long Length => throw new NotSupportedException();
        public override long Position { get => sent; set => throw new NotSupportedException(); }

        public override int Read(byte[] buffer, int offset, int count)
        {
            var n = (int)Math.Min(count, length - sent);
            Array.Fill(buffer, (byte)'x', offset, n);
            sent += n;
            return n;
        }

        public override void Flush()
        {
        }

        public override long Seek(long offset, SeekOrigin origin) => throw new NotSupportedException();

        public override void SetLength(long value) => throw new NotSupportedException();

        public override void Write(byte[] buffer, int offset, int count) => throw new NotSupportedException();
    }
}
