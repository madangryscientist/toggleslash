using System.Net;
using System.Net.Http.Json;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Limits;
using FeatureSlash.Server.Sdk;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.TestHost;
using Microsoft.Extensions.Configuration;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;

/// <summary>A clock that moves only when told to, for the limiter's <see cref="TimeProvider"/>.</summary>
public sealed class ManualClock : TimeProvider
{
    private long ticks;

    public override long TimestampFrequency => TimeSpan.TicksPerSecond;

    public override long GetTimestamp() => Interlocked.Read(ref ticks);

    public void Advance(TimeSpan by) => Interlocked.Add(ref ticks, by.Ticks);
}

/// <summary>The limiter on its own: the numbers, the rounding and the caps.</summary>
public sealed class RequestLimiterTests
{
    private readonly ManualClock clock = new();

    private RequestLimiter Limiter(int evaluate = 6000, int events = 600, int authFailures = 30, int streams = 100, int maxIps = 100_000, int maxKeys = 10_000) =>
        new(clock, new RateLimitSettings(evaluate, events, authFailures, streams) { MaxTrackedIps = maxIps, MaxTrackedKeys = maxKeys });

    private static RateLimitSettings Defaults() => RateLimitSettings.From(new ConfigurationBuilder().Build());

    private static void Spend(RequestLimiter limiter, KeyLimit limit, long keyId, int times)
    {
        for (var i = 0; i < times; i++)
        {
            Assert.True(limiter.TryTake(limit, keyId, out _), $"request {i + 1} of {times} was refused");
        }
    }

    [Fact]
    public void A_key_may_hold_100_streams_by_default() => Assert.Equal(100, Defaults().StreamsPerKey);

    [Theory]
    [InlineData(KeyLimit.Evaluate, 6000, 1)]
    [InlineData(KeyLimit.Events, 600, 1)]
    public void A_per_key_limit_trips_at_its_default_number_and_recovers(KeyLimit limit, int number, long retryAfter)
    {
        var limiter = new RequestLimiter(clock, Defaults());

        Spend(limiter, limit, 1, number);

        Assert.False(limiter.TryTake(limit, 1, out var wait));
        Assert.Equal(retryAfter, wait);
        clock.Advance(TimeSpan.FromSeconds(wait));
        Assert.True(limiter.TryTake(limit, 1, out _));
    }

    [Theory]
    [InlineData(1, 60)]
    [InlineData(7, 9)] // 8.57 seconds, rounded up
    [InlineData(60, 1)]
    [InlineData(61, 1)] // under a second, and never zero
    public void Retry_after_is_whole_seconds_rounded_up(int perMinute, long seconds)
    {
        var limiter = Limiter(evaluate: perMinute);
        Spend(limiter, KeyLimit.Evaluate, 1, perMinute);

        Assert.False(limiter.TryTake(KeyLimit.Evaluate, 1, out var wait));

        Assert.Equal(seconds, wait);
    }

    [Fact]
    public void Idle_time_never_banks_more_than_a_minutes_allowance()
    {
        var limiter = Limiter(evaluate: 5);
        Spend(limiter, KeyLimit.Evaluate, 1, 1);

        clock.Advance(TimeSpan.FromDays(365));

        Spend(limiter, KeyLimit.Evaluate, 1, 5);
        Assert.False(limiter.TryTake(KeyLimit.Evaluate, 1, out _));
    }

    [Fact]
    public void Failures_past_the_number_are_paid_back_so_concurrent_guesses_do_not_get_a_discount()
    {
        var limiter = Limiter(authFailures: 1);

        limiter.RecordAuthFailure("ip");
        limiter.RecordAuthFailure("ip");

        Assert.True(limiter.IsLockedOut("ip", out var wait));
        Assert.Equal(120, wait);
    }

    [Fact]
    public void Checking_an_ip_that_never_failed_tracks_nothing()
    {
        var limiter = Limiter();

        Assert.False(limiter.IsLockedOut("ip", out var wait));

        Assert.Equal(0, wait);
        Assert.Equal(0, limiter.Tracked.Ips);
    }

    [Fact]
    public void Rotating_ips_cannot_grow_past_the_cap_and_the_stalest_goes_first()
    {
        var limiter = Limiter(authFailures: 1, maxIps: 3);
        limiter.RecordAuthFailure("stalest");
        limiter.RecordAuthFailure("touched");
        limiter.RecordAuthFailure("third");
        Assert.True(limiter.IsLockedOut("stalest", out _)); // now the most recent, leaving "touched" stalest
        Assert.True(limiter.IsLockedOut("third", out _));

        for (var i = 0; i < 1000; i++)
        {
            limiter.RecordAuthFailure($"rotating-{i}");
            Assert.True(limiter.Tracked.Ips <= 3);
        }

        Assert.Equal(3, limiter.Tracked.Ips);
        Assert.False(limiter.IsLockedOut("touched", out _));
    }

    [Fact]
    public void Rotating_keys_cannot_grow_past_the_cap()
    {
        var limiter = Limiter(maxKeys: 3);

        for (var key = 0L; key < 1000; key++)
        {
            Assert.True(limiter.TryTake(KeyLimit.Evaluate, key, out _));
            Assert.True(limiter.TryTake(KeyLimit.Events, key, out _));
        }

        Assert.Equal(3, limiter.Tracked.EvaluateKeys);
        Assert.Equal(3, limiter.Tracked.EventKeys);
    }

    [Fact]
    public void Stream_tracking_holds_its_cap_and_forgets_a_key_with_nothing_open()
    {
        var limiter = Limiter(maxKeys: 2);
        var first = limiter.TryOpenStream(1)!;
        using var second = limiter.TryOpenStream(2);

        Assert.Null(limiter.TryOpenStream(3));
        Assert.Equal(2, limiter.Tracked.StreamKeys);

        first.Dispose();
        Assert.Equal(1, limiter.Tracked.StreamKeys);
        using var third = limiter.TryOpenStream(3);
        Assert.NotNull(third);
    }
}

/// <summary>The limits as a client meets them, through the real pipeline, on a fake clock.</summary>
public sealed class RateLimitTests : IDisposable
{
    private const string Evaluate = "/sdk/v1/evaluate";
    private const string Events = "/sdk/v1/events";
    private const string Ruleset = "/sdk/v1/ruleset";
    private const string Stream = "/sdk/v1/stream";
    private const string Projects = "/api/v1/projects";
    private const string Origin = "https://app.example";

    /// <summary>What a test sends to say which address the connection comes from; TestServer has none of its own.</summary>
    private const string PeerHeader = "X-Test-Peer";

    private static readonly TimeSpan Guard = TimeSpan.FromSeconds(10);
    private static readonly object EvaluateBody = new { context = new { key = "u1" } };
    private static readonly object EventsBody = new { events = Array.Empty<object>() };

    private readonly ManualClock clock = new();
    private readonly StreamEnds ends = new();
    private readonly List<IDisposable> started = [];

    public void Dispose()
    {
        foreach (var server in started)
        {
            server.Dispose();
        }
    }

    private ApiServer Start(params (string Setting, string Value)[] settings) => Start(null, settings);

    private ApiServer Start(Action<IServiceCollection>? services, params (string Setting, string Value)[] settings)
    {
        var server = ApiServer.With(builder =>
        {
            foreach (var (setting, value) in settings)
            {
                builder.UseSetting(setting, value);
            }
            builder.ConfigureTestServices(collection =>
            {
                collection.AddSingleton<TimeProvider>(clock);
                collection.AddSingleton<IStartupFilter>(new PeerAddress(ends));
                services?.Invoke(collection);
            });
        });
        started.Add(server);
        return server;
    }

    private static async Task<string> Seed(ApiServer server, string kind = "server")
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        return await NewKey(server, kind);
    }

    private static async Task<string> NewKey(ApiServer server, string kind = "server")
    {
        var created = await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private static async Task<HttpResponseMessage> Send(
        ApiServer server,
        HttpMethod method,
        string path,
        string? token,
        object? body = null,
        string? peer = null,
        string? forwardedFor = null,
        string? origin = null)
    {
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(method, path);
        if (token is not null)
        {
            request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {token}");
        }
        if (peer is not null)
        {
            request.Headers.TryAddWithoutValidation(PeerHeader, peer);
        }
        if (forwardedFor is not null)
        {
            request.Headers.TryAddWithoutValidation("X-Forwarded-For", forwardedFor);
        }
        if (origin is not null)
        {
            request.Headers.TryAddWithoutValidation("Origin", origin);
        }
        if (body is not null)
        {
            request.Content = JsonContent.Create(body);
        }
        return await client.SendAsync(request);
    }

    private static Task<HttpResponseMessage> Wrong(ApiServer server, string? peer = null, string? forwardedFor = null) =>
        Send(server, HttpMethod.Get, Ruleset, "fs_wrong_credential", peer: peer, forwardedFor: forwardedFor);

    private static async Task FailTimes(ApiServer server, int times, string? peer = null, string? forwardedFor = null)
    {
        for (var i = 0; i < times; i++)
        {
            await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await Wrong(server, peer, forwardedFor));
        }
    }

    private static async Task ExpectLimited(long retryAfter, HttpResponseMessage response)
    {
        await ApiServer.ExpectError(HttpStatusCode.TooManyRequests, "rate_limited", response);
        Assert.Equal([retryAfter.ToString()], response.Headers.GetValues("Retry-After"));
    }

    // --- per key ------------------------------------------------------------------------------------------

    [Fact]
    public async Task Evaluate_trips_at_its_number_answers_with_retry_after_and_recovers()
    {
        var server = Start((RateLimitSettings.EvaluateSetting, "3"));
        var key = await Seed(server, "client");

        for (var i = 0; i < 3; i++)
        {
            await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, Evaluate, key, EvaluateBody));
        }

        await ExpectLimited(20, await Send(server, HttpMethod.Post, Evaluate, key, EvaluateBody));
        clock.Advance(TimeSpan.FromSeconds(19));
        await ExpectLimited(1, await Send(server, HttpMethod.Post, Evaluate, key, EvaluateBody));
        clock.Advance(TimeSpan.FromSeconds(1));
        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, Evaluate, key, EvaluateBody));
    }

    [Fact]
    public async Task Events_trips_at_its_number_answers_with_retry_after_and_recovers()
    {
        var server = Start((RateLimitSettings.EventsSetting, "2"));
        var key = await Seed(server, "client");

        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, Events, key, EventsBody));
        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, Events, key, EventsBody));

        await ExpectLimited(30, await Send(server, HttpMethod.Post, Events, key, EventsBody));
        clock.Advance(TimeSpan.FromSeconds(30));
        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, Events, key, EventsBody));
    }

    [Fact]
    public async Task One_key_being_limited_does_not_touch_another_key_or_the_other_route()
    {
        var server = Start((RateLimitSettings.EvaluateSetting, "1"));
        var key = await Seed(server, "client");
        var other = await NewKey(server, "client");

        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, Evaluate, key, EvaluateBody));
        await ExpectLimited(60, await Send(server, HttpMethod.Post, Evaluate, key, EvaluateBody));

        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, Evaluate, other, EvaluateBody));
        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, Events, key, EventsBody));
    }

    [Fact]
    public async Task A_limited_key_is_refused_before_its_request_is_read()
    {
        var server = Start((RateLimitSettings.EvaluateSetting, "1"));
        var key = await Seed(server, "client");
        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, Evaluate, key, EvaluateBody));

        await ExpectLimited(60, await Send(server, HttpMethod.Post, Evaluate, key, new { notAContext = true }));
    }

    [Fact]
    public async Task The_ruleset_and_the_admin_routes_have_no_request_limit()
    {
        var server = Start((RateLimitSettings.EvaluateSetting, "1"), (RateLimitSettings.EventsSetting, "1"));
        var key = await Seed(server);

        for (var i = 0; i < 5; i++)
        {
            await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, Ruleset, key));
            await ApiServer.Expect(HttpStatusCode.OK, await server.Get(Projects));
        }
    }

    // --- failed authentication ----------------------------------------------------------------------------

    [Fact]
    public async Task An_ip_is_refused_at_the_default_number_and_recovers()
    {
        var server = Start();
        var key = await Seed(server);

        await FailTimes(server, 30, "198.51.100.1");

        await ExpectLimited(2, await Wrong(server, "198.51.100.1"));
        clock.Advance(TimeSpan.FromSeconds(2));
        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, Ruleset, key, peer: "198.51.100.1"));
    }

    [Fact]
    public async Task An_over_limit_ip_gets_429_even_with_a_correct_credential_on_every_kind_of_route()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "2"));
        var key = await Seed(server);
        const string Attacker = "198.51.100.2";
        await FailTimes(server, 2, Attacker);

        await ExpectLimited(30, await Send(server, HttpMethod.Get, Ruleset, key, peer: Attacker));
        await ExpectLimited(30, await Send(server, HttpMethod.Get, Stream, key, peer: Attacker));
        await ExpectLimited(30, await Send(server, HttpMethod.Get, Projects, ApiServer.AdminToken, peer: Attacker));
        Assert.False((await Send(server, HttpMethod.Get, Projects, ApiServer.AdminToken, peer: Attacker)).Headers.Contains("WWW-Authenticate"));
    }

    [Fact]
    public async Task One_ip_being_locked_out_does_not_touch_another()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"));
        var key = await Seed(server);
        await FailTimes(server, 1, "198.51.100.3");
        await ExpectLimited(60, await Wrong(server, "198.51.100.3"));

        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, Ruleset, key, peer: "198.51.100.4"));
        await FailTimes(server, 1, "198.51.100.4");
    }

    [Fact]
    public async Task Failures_on_admin_routes_count_too()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"));

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await Send(server, HttpMethod.Get, Projects, "wrong", peer: "198.51.100.5"));

        await ExpectLimited(60, await Send(server, HttpMethod.Get, Projects, ApiServer.AdminToken, peer: "198.51.100.5"));
    }

    [Fact]
    public async Task A_header_that_is_not_a_bearer_token_is_a_presented_credential()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"));
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Get, Projects);
        request.Headers.TryAddWithoutValidation("Authorization", "Basic abc");
        request.Headers.TryAddWithoutValidation(PeerHeader, "198.51.100.6");

        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await client.SendAsync(request));

        await ExpectLimited(60, await Send(server, HttpMethod.Get, Projects, ApiServer.AdminToken, peer: "198.51.100.6"));
    }

    [Fact]
    public async Task A_missing_header_does_not_count_as_a_failure()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "2"));
        var key = await Seed(server);

        for (var i = 0; i < 10; i++)
        {
            await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", await Send(server, HttpMethod.Get, Ruleset, null, peer: "198.51.100.7"));
        }

        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, Ruleset, key, peer: "198.51.100.7"));
    }

    [Fact]
    public async Task The_503_for_an_unset_admin_token_does_not_count_and_is_not_replaced()
    {
        var server = Start((ServerAuth.AdminTokenSetting, ""), (RateLimitSettings.AuthFailuresSetting, "1"));

        for (var i = 0; i < 5; i++)
        {
            await ApiServer.ExpectError(HttpStatusCode.ServiceUnavailable, "admin_token_unset", await Send(server, HttpMethod.Get, Projects, "anything", peer: "198.51.100.8"));
        }

        await FailTimes(server, 1, "198.51.100.8");
        await ExpectLimited(60, await Wrong(server, "198.51.100.8"));
        await ApiServer.ExpectError(HttpStatusCode.ServiceUnavailable, "admin_token_unset", await Send(server, HttpMethod.Get, Projects, "anything", peer: "198.51.100.8"));
    }

    [Fact]
    public async Task A_valid_credential_on_the_wrong_route_is_not_a_failure()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"));
        var client = await Seed(server, "client");

        for (var i = 0; i < 3; i++)
        {
            await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await Send(server, HttpMethod.Get, Ruleset, client, peer: "198.51.100.9"));
        }
    }

    [Fact]
    public async Task Health_is_never_limited_and_counts_nothing()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"));
        await FailTimes(server, 1, "198.51.100.10");
        await ExpectLimited(60, await Wrong(server, "198.51.100.10"));

        for (var i = 0; i < 5; i++)
        {
            await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, "/health", "wrong", peer: "198.51.100.10"));
            await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, "/health", "wrong", peer: "198.51.100.11"));
        }

        await FailTimes(server, 1, "198.51.100.11");
    }

    // --- the client's address -----------------------------------------------------------------------------

    [Fact]
    public async Task Ipv6_groups_by_the_64_so_rotating_inside_one_does_not_escape()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"));
        var key = await Seed(server);
        await FailTimes(server, 1, "2001:db8:1:2::1");

        await ExpectLimited(60, await Send(server, HttpMethod.Get, Ruleset, key, peer: "2001:db8:1:2:ffff:ffff:ffff:ffff"));

        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, Ruleset, key, peer: "2001:db8:1:3::1"));
    }

    [Fact]
    public async Task An_ipv4_client_on_a_dual_stack_socket_is_the_same_client()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"));
        var key = await Seed(server);
        await FailTimes(server, 1, "::ffff:198.51.100.12");

        await ExpectLimited(60, await Send(server, HttpMethod.Get, Ruleset, key, peer: "198.51.100.12"));

        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, Ruleset, key, peer: "::ffff:198.51.100.13"));
    }

    [Fact]
    public async Task Forwarded_for_is_ignored_with_no_proxies_listed()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"));
        var key = await Seed(server);
        await FailTimes(server, 1, "198.51.100.14", forwardedFor: "203.0.113.1");

        await ExpectLimited(60, await Send(server, HttpMethod.Get, Ruleset, key, peer: "198.51.100.14", forwardedFor: "203.0.113.2"));
        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, Ruleset, key, peer: "198.51.100.15", forwardedFor: "198.51.100.14"));
    }

    [Fact]
    public async Task Forwarded_for_is_ignored_from_an_unlisted_address()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"), (RateLimitSettings.TrustedProxiesSetting, "10.0.0.1"));
        var key = await Seed(server);
        await FailTimes(server, 1, "10.0.0.2", forwardedFor: "203.0.113.1");

        await ExpectLimited(60, await Send(server, HttpMethod.Get, Ruleset, key, peer: "10.0.0.2", forwardedFor: "203.0.113.2"));
    }

    [Fact]
    public async Task Forwarded_for_from_a_listed_proxy_tells_clients_apart()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"), (RateLimitSettings.TrustedProxiesSetting, "10.0.0.1, 10.0.0.2"));
        var key = await Seed(server);
        await FailTimes(server, 1, "10.0.0.1", forwardedFor: "203.0.113.1");

        await ExpectLimited(60, await Send(server, HttpMethod.Get, Ruleset, key, peer: "10.0.0.2", forwardedFor: "203.0.113.1"));
        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Get, Ruleset, key, peer: "10.0.0.1", forwardedFor: "203.0.113.2"));
    }

    [Fact]
    public async Task A_client_cannot_write_its_way_to_another_address_through_a_listed_proxy()
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"), (RateLimitSettings.TrustedProxiesSetting, "10.0.0.1"));
        var key = await Seed(server);
        await FailTimes(server, 1, "10.0.0.1", forwardedFor: "1.1.1.1, 203.0.113.1");

        await ExpectLimited(60, await Send(server, HttpMethod.Get, Ruleset, key, peer: "10.0.0.1", forwardedFor: "2.2.2.2, 203.0.113.1"));
    }

    // --- from a browser -----------------------------------------------------------------------------------

    [Theory]
    [InlineData(Evaluate, RateLimitSettings.EvaluateSetting)]
    [InlineData(Events, RateLimitSettings.EventsSetting)]
    public async Task A_browser_can_read_a_429_and_its_retry_after(string path, string setting)
    {
        var server = Start((setting, "1"));
        var key = await Seed(server, "client");
        var body = path == Evaluate ? EvaluateBody : EventsBody;
        await ApiServer.Expect(HttpStatusCode.OK, await Send(server, HttpMethod.Post, path, key, body, origin: Origin));

        var response = await Send(server, HttpMethod.Post, path, key, body, origin: Origin);

        Assert.Equal(["*"], response.Headers.GetValues("Access-Control-Allow-Origin"));
        Assert.Contains("Retry-After", response.Headers.GetValues("Access-Control-Expose-Headers"));
        await ExpectLimited(60, response);
    }

    [Theory]
    [InlineData(Evaluate)]
    [InlineData(Events)]
    public async Task A_browser_can_read_an_ip_lockout_too(string path)
    {
        var server = Start((RateLimitSettings.AuthFailuresSetting, "1"));
        var key = await Seed(server, "client");
        var body = path == Evaluate ? EvaluateBody : EventsBody;
        await ApiServer.Expect(HttpStatusCode.Unauthorized, await Send(server, HttpMethod.Post, path, "wrong", body, peer: "198.51.100.16", origin: Origin));

        var response = await Send(server, HttpMethod.Post, path, key, body, peer: "198.51.100.16", origin: Origin);

        Assert.Equal(["*"], response.Headers.GetValues("Access-Control-Allow-Origin"));
        Assert.Contains("Retry-After", response.Headers.GetValues("Access-Control-Expose-Headers"));
        await ExpectLimited(60, response);
    }

    // --- streams ------------------------------------------------------------------------------------------

    private static async Task<(HttpClient Client, HttpResponseMessage Response)> OpenStream(ApiServer server, string key)
    {
        var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Get, Stream);
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {key}");
        var response = await client.SendAsync(request, HttpCompletionOption.ResponseHeadersRead).WaitAsync(Guard);
        return (client, response);
    }

    private static void Close((HttpClient Client, HttpResponseMessage Response) stream)
    {
        stream.Response.Dispose();
        stream.Client.Dispose();
    }

    [Fact]
    public async Task Streams_stop_at_the_limit_for_a_key_and_a_disconnect_gives_the_slot_back()
    {
        var server = Start((RateLimitSettings.StreamsSetting, "2"));
        var key = await Seed(server);
        var other = await NewKey(server);
        var first = await OpenStream(server, key);
        var second = await OpenStream(server, key);
        Assert.Equal(HttpStatusCode.OK, first.Response.StatusCode);
        Assert.Equal(HttpStatusCode.OK, second.Response.StatusCode);

        var refused = await OpenStream(server, key);
        await ExpectLimited(RequestLimiter.StreamRetryAfterSeconds, refused.Response);
        await ends.Finished.WaitAsync(Guard);
        Close(refused);

        var otherKey = await OpenStream(server, other);
        Assert.Equal(HttpStatusCode.OK, otherKey.Response.StatusCode);

        Close(first);
        await ends.Finished.WaitAsync(Guard);

        var third = await OpenStream(server, key);
        Assert.Equal(HttpStatusCode.OK, third.Response.StatusCode);

        Close(second);
        Close(third);
        Close(otherKey);
    }

    [Fact]
    public async Task A_stream_that_fails_gives_its_slot_back()
    {
        var heartbeat = new FailingHeartbeat();
        var server = Start(services => services.AddSingleton<StreamHeartbeat>(heartbeat), (RateLimitSettings.StreamsSetting, "1"));
        var key = await Seed(server);
        var limiter = server.Server.Services.GetRequiredService<RequestLimiter>();
        var stream = await OpenStream(server, key);
        Assert.Equal(1, limiter.Tracked.StreamKeys);

        heartbeat.Fail();
        await ends.Finished.WaitAsync(Guard);

        Assert.Equal(0, limiter.Tracked.StreamKeys);
        Close(stream);
        var next = await OpenStream(server, key);
        Assert.Equal(HttpStatusCode.OK, next.Response.StatusCode);
        Close(next);
    }

    /// <summary>On real Kestrel, because a test host does not stop the way a real one does.</summary>
    [Fact]
    public async Task Server_shutdown_gives_stream_slots_back()
    {
        using var real = new ApiServer();
        real.Server.UseKestrel(0);
        real.Server.StartServer();
        var key = await Seed(real);
        var limiter = real.Server.Services.GetRequiredService<RequestLimiter>();
        var stream = await OpenStream(real, key);
        Assert.Equal(HttpStatusCode.OK, stream.Response.StatusCode);
        Assert.Equal(1, limiter.Tracked.StreamKeys);
        var lifetime = real.Server.Services.GetRequiredService<IHostApplicationLifetime>();
        var stopped = new TaskCompletionSource();
        using var registration = lifetime.ApplicationStopped.Register(stopped.SetResult);

        lifetime.StopApplication();

        await stopped.Task.WaitAsync(Guard);
        Assert.Equal(0, limiter.Tracked.StreamKeys);
        Close(stream);
    }

    // --- settings -----------------------------------------------------------------------------------------

    public static TheoryData<string, string> BadSettings()
    {
        // All four go through one parser: every bad value on one variable, and one bad value on the others.
        var data = new TheoryData<string, string>();
        foreach (var value in new[] { "0", "-1", "abc", "", "1.5", " 5", "+5", "1e3", "99999999999" })
        {
            data.Add(RateLimitSettings.EvaluateSetting, value);
        }
        foreach (var setting in new[] { RateLimitSettings.EventsSetting, RateLimitSettings.AuthFailuresSetting, RateLimitSettings.StreamsSetting })
        {
            data.Add(setting, "abc");
        }
        return data;
    }

    [Theory]
    [MemberData(nameof(BadSettings))]
    public void An_invalid_limit_fails_startup_naming_the_variable(string setting, string value)
    {
        using var database = new TempDatabase();
        using var server = new ServerOn(database.Path, builder => builder.UseSetting(setting, value));

        var error = Assert.Throws<InvalidOperationException>(server.Start);

        Assert.Contains(setting, error.Message);
        Assert.False(File.Exists(database.Path), "a bad limit must stop the server before it touches the database");
    }

    [Theory]
    [InlineData("not-an-address")]
    [InlineData("10.0.0.1, nope")]
    [InlineData("10.0.0.0/8")]
    public void An_invalid_proxy_list_fails_startup_naming_the_variable(string value)
    {
        using var database = new TempDatabase();
        using var server = new ServerOn(database.Path, builder => builder.UseSetting(RateLimitSettings.TrustedProxiesSetting, value));

        var error = Assert.Throws<InvalidOperationException>(server.Start);

        Assert.Contains(RateLimitSettings.TrustedProxiesSetting, error.Message);
    }

    // --- what is kept -------------------------------------------------------------------------------------

    [Fact]
    public async Task Neither_a_keys_plaintext_nor_an_ip_reaches_a_log_or_the_database()
    {
        const string Proxy = "10.77.0.1";
        const string Unlisted = "10.77.0.2";
        const string Client = "203.0.113.77";
        const string Prefix = "2001:db8:77:77";
        var logs = new CapturedLogs();
        using var database = new TempDatabase();
        string key;
        const string WrongKey = "fs_wrong_credential_that_must_not_be_logged";
        using (var server = ApiServerOn(database, logs, clock, ends))
        {
            key = await Seed(server, "server");

            await Send(server, HttpMethod.Post, Evaluate, key, EvaluateBody, peer: Proxy, forwardedFor: Client);
            await Send(server, HttpMethod.Post, Evaluate, key, EvaluateBody, peer: Proxy, forwardedFor: Client);
            await Send(server, HttpMethod.Post, Events, key, EventsBody, peer: Proxy, forwardedFor: Client);
            await Send(server, HttpMethod.Post, Events, key, EventsBody, peer: Proxy, forwardedFor: Client);
            await Send(server, HttpMethod.Get, Ruleset, WrongKey, peer: Proxy, forwardedFor: Client);
            await Send(server, HttpMethod.Get, Ruleset, key, peer: Proxy, forwardedFor: Client);
            await Send(server, HttpMethod.Get, Ruleset, WrongKey, peer: Unlisted, forwardedFor: Client);
            await Send(server, HttpMethod.Get, Ruleset, WrongKey, peer: $"{Prefix}::1");
            await Send(server, HttpMethod.Get, Ruleset, key, peer: $"{Prefix}::1");
            var refused = await Send(server, HttpMethod.Get, Ruleset, key, peer: Proxy, forwardedFor: Client);
            await ExpectLimited(60, refused);
        }

        var stored = string.Concat(Directory.GetFiles(Path.GetDirectoryName(database.Path)!).Select(File.ReadAllBytes).Select(Convert.ToHexString));
        foreach (var secret in new[] { key, WrongKey, Client, Proxy, Unlisted, Prefix })
        {
            Assert.DoesNotContain(secret, logs.All);
            Assert.DoesNotContain(Convert.ToHexString(System.Text.Encoding.UTF8.GetBytes(secret)), stored);
        }
    }

    /// <summary>A server that logs everything, with low limits, on the caller's database, closed before its file is read.</summary>
    private static ApiServer ApiServerOn(TempDatabase database, CapturedLogs logs, ManualClock clock, StreamEnds ends) =>
        ApiServer.With(builder =>
        {
            builder.UseSetting(FeatureSlash.Server.Data.ServerDatabase.PathSetting, database.Path);
            builder.UseSetting(RateLimitSettings.EvaluateSetting, "1");
            builder.UseSetting(RateLimitSettings.EventsSetting, "1");
            builder.UseSetting(RateLimitSettings.AuthFailuresSetting, "1");
            builder.UseSetting(RateLimitSettings.TrustedProxiesSetting, "10.77.0.1");
            builder.ConfigureLogging(logging => logging.SetMinimumLevel(LogLevel.Trace).AddProvider(logs));
            builder.ConfigureTestServices(services =>
            {
                services.AddSingleton<TimeProvider>(clock);
                services.AddSingleton<IStartupFilter>(new PeerAddress(ends));
            });
        });

    private sealed class FailingHeartbeat : StreamHeartbeat
    {
        private readonly TaskCompletionSource next = new(TaskCreationOptions.RunContinuationsAsynchronously);

        public void Fail() => next.TrySetException(new InvalidOperationException("the heartbeat fails"));

        public override Task Next(CancellationToken cancellation) => next.Task.WaitAsync(cancellation);
    }

    /// <summary>Reports each stream request that has finished, by which point its slot must be back.</summary>
    private sealed class StreamEnds
    {
        public SemaphoreSlim Finished { get; } = new(0);
    }

    /// <summary>
    /// Ahead of the whole pipeline, as a socket is: gives the connection the address a test named, and reports a
    /// stream request ending.
    /// </summary>
    private sealed class PeerAddress(StreamEnds ends) : IStartupFilter
    {
        public Action<IApplicationBuilder> Configure(Action<IApplicationBuilder> next) => app =>
        {
            app.Use(async (context, inner) =>
            {
                if (context.Request.Headers.TryGetValue(PeerHeader, out var peer))
                {
                    context.Connection.RemoteIpAddress = IPAddress.Parse(peer.ToString());
                }
                try
                {
                    await inner(context);
                }
                finally
                {
                    if (context.Request.Path == Stream && context.Response.StatusCode != StatusCodes.Status401Unauthorized)
                    {
                        ends.Finished.Release();
                    }
                }
            });
            next(app);
        };
    }
}
