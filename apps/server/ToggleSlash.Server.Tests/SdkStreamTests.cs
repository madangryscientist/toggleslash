using System.Data.Common;
using System.Net;
using System.Text.Json;
using System.Threading.Channels;
using ToggleSlash.Server.Data;
using ToggleSlash.Server.Sdk;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.TestHost;
using Microsoft.EntityFrameworkCore;
using Microsoft.EntityFrameworkCore.Diagnostics;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;

/// <summary>
/// <c>GET /sdk/v1/stream</c>. Nothing here waits for time to pass: the heartbeat is ticked by hand, and
/// <see cref="Guard"/> only bounds a wait so that a broken stream fails instead of hanging.
/// </summary>
public sealed class SdkStreamTests : IDisposable
{
    private const string StreamPath = "/sdk/v1/stream";
    private const string Ruleset = "/sdk/v1/ruleset";
    private const string Flags = "/api/v1/projects/shop/flags";
    private const string ProdSegments = "/api/v1/projects/shop/environments/prod/segments";

    private static readonly TimeSpan Guard = TimeSpan.FromSeconds(10);

    private readonly ManualHeartbeat heartbeat = new();
    private readonly StreamProbe probe = new();
    private readonly CommitSwitch commits = new();
    private readonly CapturedLogs logs = new();
    private readonly ApiServer server;

    public SdkStreamTests() =>
        server = ApiServer.With(builder => builder.ConfigureLogging(logging => logging.SetMinimumLevel(LogLevel.Trace).AddProvider(logs)).ConfigureTestServices(services =>
        {
            services.AddSingleton<StreamHeartbeat>(heartbeat);
            services.AddSingleton<IStartupFilter>(probe);
            services.ConfigureDbContext<ToggleSlashDbContext>(options => options.AddInterceptors(commits));
        }));

    public void Dispose() => server.Dispose();

    private RulesetNotifier Notifier => server.Server.Services.GetRequiredService<RulesetNotifier>();

    private static string Config(string flag, string env = "prod") => $"{Flags}/{flag}/environments/{env}";

    private static object Off(long version) =>
        new { on = false, offVariation = 1, targets = Array.Empty<object>(), prerequisites = Array.Empty<object>(), rules = Array.Empty<object>(), fallthrough = new { variation = 0 }, version };

    private static object NoMembers(string? key = null) => key is null
        ? new { included = Array.Empty<string>(), excluded = Array.Empty<string>(), rules = Array.Empty<object>() }
        : new { key, included = Array.Empty<string>(), excluded = Array.Empty<string>(), rules = Array.Empty<object>() };

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedEnvironment("shop", "staging");
        await server.SeedFlag("shop", "dark-mode");
    }

    private Task<string> ServerKey(string env = "prod") => Key(server, "server", env);

    private static async Task<string> Key(ApiServer on, string kind, string env = "prod")
    {
        var created = await on.Post($"/api/v1/projects/shop/environments/{env}/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private async Task<string> ETagOf(string key) => await ETagOf(server, key);

    private static async Task<string> ETagOf(ApiServer on, string key)
    {
        var response = await on.Send(HttpMethod.Get, Ruleset, $"Bearer {key}");
        await ApiServer.Expect(HttpStatusCode.OK, response);
        return response.Headers.ETag!.ToString();
    }

    private async Task Toggle(bool on, string env = "prod") =>
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Config("dark-mode", env)}/toggle", new { on }));

    private long EnvironmentId(string env)
    {
        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT id FROM environments WHERE key = $key";
        command.Parameters.AddWithValue("$key", env);
        return (long)command.ExecuteScalar()!;
    }

    private Task<OpenStream> Open(string key) => OpenStream.Connect(server, key);

    // --- connecting -----------------------------------------------------------------------------------------

    [Fact]
    public async Task Connecting_sends_the_current_rulesets_etag()
    {
        await Seed();
        var key = await ServerKey();

        using var stream = await Open(key);

        Assert.Equal("text/event-stream", stream.Response.Content.Headers.ContentType!.MediaType);
        Assert.True(stream.Response.Headers.CacheControl!.NoCache);
        var connected = await stream.NextEvent();
        Assert.Equal("connected", connected.Name);
        Assert.Equal(await ETagOf(key), connected.Data);
    }

    [Fact]
    public async Task A_client_key_cannot_open_the_stream()
    {
        await Seed();
        var client = await Key(server, "client");

        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(HttpMethod.Get, StreamPath, $"Bearer {client}"));
        Assert.Equal(0, Notifier.SubscriberCount);
    }

    // --- a write ---------------------------------------------------------------------------------------------

    [Fact]
    public async Task A_write_sends_a_changed_event_with_the_new_etag()
    {
        await Seed();
        var key = await ServerKey();
        using var stream = await Open(key);
        var connected = await stream.NextEvent();

        await Toggle(on: true);

        var changed = await stream.NextEvent();
        Assert.Equal("changed", changed.Name);
        Assert.NotEqual(connected.Data, changed.Data);
        Assert.Equal(await ETagOf(key), changed.Data);
    }

    [Fact]
    public async Task A_write_to_another_environment_does_not_reach_this_one()
    {
        await Seed();
        var prodKey = await ServerKey();
        var stagingKey = await ServerKey("staging");
        using var prod = await Open(prodKey);
        using var staging = await Open(stagingKey);
        await prod.NextEvent();
        await staging.NextEvent();
        using var prodSignal = Notifier.Subscribe(EnvironmentId("prod"));
        using var stagingSignal = Notifier.Subscribe(EnvironmentId("staging"));

        await Toggle(on: true, env: "staging");

        Assert.Equal(await ETagOf(stagingKey), (await staging.NextEvent()).Data);
        await stagingSignal.Next(default).WaitAsync(Guard);
        Assert.False(prodSignal.Next(default).IsCompleted);

        // The first thing prod hears is prod's own write.
        await Toggle(on: true);
        Assert.Equal(await ETagOf(prodKey), (await prod.NextEvent()).Data);
    }

    [Fact]
    public async Task A_failed_write_sends_nothing()
    {
        await Seed();
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(ProdSegments, NoMembers("kept")));
        var key = await ServerKey();
        using var stream = await Open(key);
        await stream.NextEvent();
        using var signal = Notifier.Subscribe(EnvironmentId("prod"));

        // Refused: the version is stale.
        await ApiServer.Expect(HttpStatusCode.Conflict, await server.Put(Config("dark-mode"), Off(version: 99)));
        // Refused: the segment's version is stale.
        await ApiServer.Expect(HttpStatusCode.Conflict, await server.Put($"{ProdSegments}/kept", new { included = Array.Empty<string>(), excluded = Array.Empty<string>(), rules = Array.Empty<object>(), version = 99 }));
        // Refused: the segment is not there.
        await ApiServer.Expect(HttpStatusCode.NotFound, await server.Delete($"{ProdSegments}/nope"));
        // A signal these raised would be taken here, at once: Next only waits when nothing is pending.
        var next = signal.Next(default);
        Assert.False(next.IsCompleted);

        await Toggle(on: true);

        await next.WaitAsync(Guard);
        Assert.Equal(await ETagOf(key), (await stream.NextEvent()).Data);
    }

    /// <summary>
    /// A commit that fails after everything else has succeeded leaves no signal, and the same write with the
    /// commit allowed does: so each route raises it after the commit, not before.
    /// </summary>
    [Theory]
    [InlineData("config")]
    [InlineData("toggle")]
    [InlineData("segment-create")]
    [InlineData("segment-put")]
    [InlineData("segment-delete")]
    [InlineData("flag-create")]
    [InlineData("flag-archive")]
    [InlineData("flag-restore")]
    public async Task Each_ruleset_write_signals_after_its_commit_and_only_then(string write)
    {
        await Seed();
        await server.SeedFlag("shop", "retired");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flags}/retired/archive"));
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post(ProdSegments, NoMembers("kept")));
        var environmentId = EnvironmentId("prod");
        var version = (await ApiServer.Json(await server.Get(Config("dark-mode")))).GetProperty("version").GetInt64();

        Task<HttpResponseMessage> Send() => write switch
        {
            "config" => server.Put(Config("dark-mode"), Off(version)),
            "toggle" => server.Post($"{Config("dark-mode")}/toggle", new { on = true }),
            "segment-create" => server.Post(ProdSegments, NoMembers("fresh")),
            "segment-put" => server.Put($"{ProdSegments}/kept", new { included = Array.Empty<string>(), excluded = Array.Empty<string>(), rules = Array.Empty<object>(), version = 1 }),
            "segment-delete" => server.Delete($"{ProdSegments}/kept"),
            "flag-create" => server.Post(Flags, new { key = "fresh", name = "fresh", variations = new[] { true, false } }),
            "flag-archive" => server.Post($"{Flags}/dark-mode/archive"),
            "flag-restore" => server.Post($"{Flags}/retired/restore"),
            _ => throw new ArgumentOutOfRangeException(nameof(write)),
        };

        using var signal = Notifier.Subscribe(environmentId);

        commits.Failing = true;
        await ApiServer.Expect(HttpStatusCode.InternalServerError, await Send());
        commits.Failing = false;
        var next = signal.Next(default);
        Assert.False(next.IsCompleted);

        var response = await Send();
        Assert.True(response.IsSuccessStatusCode, $"{write}: {response.StatusCode}");
        await next.WaitAsync(Guard);
    }

    [Fact]
    public async Task A_write_that_leaves_the_ruleset_as_it_was_sends_no_event()
    {
        await Seed();
        await server.SeedFlag("shop", "retired");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flags}/retired/archive"));
        var key = await ServerKey();
        using var stream = await Open(key);
        await stream.NextEvent();

        // Archiving an archived flag succeeds and signals; the served bytes are the same.
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Flags}/retired/archive"));
        await Toggle(on: true);

        var next = await stream.NextEvent();
        Assert.Equal(await ETagOf(key), next.Data);
        heartbeat.Tick();
        await stream.NextKeepAlive();
    }

    // --- a slow client ---------------------------------------------------------------------------------------

    /// <summary>
    /// At this level because a stream cannot show it: same-ETag suppression hides any extra wake-ups an
    /// unbounded channel would queue, so only this fails if the bound goes.
    /// </summary>
    [Fact]
    public async Task A_burst_of_signals_is_one_wake_up()
    {
        var notifier = new RulesetNotifier();
        using var subscription = notifier.Subscribe(1);

        foreach (var _ in Enumerable.Range(0, 1000))
        {
            notifier.Changed(1);
        }

        Assert.True(subscription.Next(default).IsCompletedSuccessfully);
        Assert.False(subscription.Next(default).IsCompleted);
    }

    [Fact]
    public async Task A_stuck_client_holds_up_no_writer_and_then_gets_only_the_latest_etag()
    {
        await Seed();
        var key = await ServerKey();
        using var stream = await Open(key);
        await stream.NextEvent();

        // The stream is now stuck writing this event, as it would be on a client that stopped reading.
        probe.Stop();
        await Toggle(on: true);
        await probe.Stuck.WaitAsync(Guard);
        var stuck = await ETagOf(key);

        foreach (var index in Enumerable.Range(0, 20))
        {
            await Toggle(on: index % 2 == 0).WaitAsync(Guard);
        }
        var latest = await ETagOf(key);
        Assert.NotEqual(stuck, latest);

        probe.Resume();

        Assert.Equal(stuck, (await stream.NextEvent()).Data);
        Assert.Equal(latest, (await stream.NextEvent()).Data);
        // Nothing else was queued: the next line is the keep-alive, not one of the twenty.
        heartbeat.Tick();
        await stream.NextKeepAlive();

        await Toggle(on: false);
        Assert.Equal(await ETagOf(key), (await stream.NextEvent()).Data);
    }

    // --- the end of a stream ---------------------------------------------------------------------------------

    [Fact]
    public async Task A_client_that_leaves_takes_its_subscriber_with_it()
    {
        await Seed();
        var key = await ServerKey();
        var first = await Open(key);
        var second = await Open(key);
        await first.NextEvent();
        await second.NextEvent();
        Assert.Equal(2, Notifier.SubscriberCount);

        first.Dispose();
        await probe.Finished.WaitAsync(Guard);
        Assert.Equal(1, Notifier.SubscriberCount);

        second.Dispose();
        await probe.Finished.WaitAsync(Guard);
        Assert.Equal(0, Notifier.SubscriberCount);
    }

    /// <summary>On real Kestrel, which waits for open requests when it stops (30 seconds by default, far past the guard).</summary>
    [Fact]
    public async Task Stopping_a_real_server_with_a_stream_open_completes_promptly()
    {
        using var real = new ApiServer();
        real.Server.UseKestrel(0);
        real.Server.StartServer();
        await real.SeedProject();
        await real.SeedEnvironment("shop", "prod");
        using var stream = await OpenStream.Connect(real, await Key(real, "server"));
        await stream.NextEvent();
        var lifetime = real.Server.Services.GetRequiredService<IHostApplicationLifetime>();
        var stopped = new TaskCompletionSource();
        using var registration = lifetime.ApplicationStopped.Register(stopped.SetResult);

        lifetime.StopApplication();

        await stopped.Task.WaitAsync(Guard);
        await stream.Ended();
        Assert.Equal(0, real.Server.Services.GetRequiredService<RulesetNotifier>().SubscriberCount);
    }

    [Fact]
    public async Task A_key_revoked_mid_stream_is_dropped_at_the_next_keep_alive()
    {
        await Seed();
        var (key, revoke) = await RevocableKey();
        using var stream = await Open(key);
        await stream.NextEvent();
        heartbeat.Tick();
        await stream.NextKeepAlive();

        await revoke();
        heartbeat.Tick();

        await stream.Ended();
        await probe.Finished.WaitAsync(Guard);
        Assert.Equal(0, Notifier.SubscriberCount);
    }

    [Fact]
    public async Task A_key_revoked_mid_stream_is_dropped_at_the_next_event_without_seeing_it()
    {
        await Seed();
        var (key, revoke) = await RevocableKey();
        using var stream = await Open(key);
        await stream.NextEvent();

        await revoke();
        await Toggle(on: true);

        await stream.Ended();
        await probe.Finished.WaitAsync(Guard);
        Assert.Equal(0, Notifier.SubscriberCount);
    }

    [Fact]
    public async Task A_deleted_environments_stream_ends_at_the_next_keep_alive_with_no_error_logged()
    {
        await Seed();
        var (key, revoke) = await RevocableKey();
        using var stream = await Open(key);
        await stream.NextEvent();
        await revoke();
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete("/api/v1/projects/shop/environments/prod"));

        heartbeat.Tick();

        await stream.Ended();
        await probe.Finished.WaitAsync(Guard);
        Assert.Equal(0, Notifier.SubscriberCount);
        Assert.DoesNotContain(" Error ", logs.All);
        Assert.DoesNotContain(" Critical ", logs.All);
    }

    private async Task<(string Key, Func<Task> Revoke)> RevocableKey()
    {
        var created = await ApiServer.Json(await server.Post("/api/v1/projects/shop/environments/prod/keys", new { kind = "server", name = "stream" }));
        var id = created.GetProperty("id").GetInt64();
        return (created.GetProperty("key").GetString()!, async () =>
            await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"/api/v1/projects/shop/environments/prod/keys/{id}")));
    }

    // --- keep-alive ------------------------------------------------------------------------------------------

    [Fact]
    public void A_keep_alive_is_due_every_25_seconds() => Assert.Equal(TimeSpan.FromSeconds(25), StreamHeartbeat.Interval);

    // --- the harness -----------------------------------------------------------------------------------------

    /// <summary>A client reading the stream line by line, as far as the bytes have arrived.</summary>
    private sealed class OpenStream(HttpClient client, HttpResponseMessage response, StreamReader reader) : IDisposable
    {
        public HttpResponseMessage Response => response;

        public static async Task<OpenStream> Connect(ApiServer server, string key)
        {
            var client = server.Server.CreateClient();
            using var request = new HttpRequestMessage(HttpMethod.Get, StreamPath);
            request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {key}");
            var response = await client.SendAsync(request, HttpCompletionOption.ResponseHeadersRead).WaitAsync(Guard);
            Assert.Equal(HttpStatusCode.OK, response.StatusCode);
            return new OpenStream(client, response, new StreamReader(await response.Content.ReadAsStreamAsync()));
        }

        public async Task<string?> NextLine() => await reader.ReadLineAsync().WaitAsync(Guard);

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

    /// <summary>Fails the commit of any transaction while <see cref="Failing"/> is set.</summary>
    private sealed class CommitSwitch : DbTransactionInterceptor
    {
        public volatile bool Failing;

        public override ValueTask<InterceptionResult> TransactionCommittingAsync(DbTransaction transaction, TransactionEventData eventData, InterceptionResult result, CancellationToken cancellationToken) =>
            Failing ? throw new InvalidOperationException("the commit fails") : base.TransactionCommittingAsync(transaction, eventData, result, cancellationToken);
    }

    /// <summary>
    /// Sits around the stream route: reports when a stream's request has finished, by which point its
    /// subscriber must be gone, and can stop its writes reaching the client, as a client that stopped reading would.
    /// </summary>
    private sealed class StreamProbe : IStartupFilter
    {
        private readonly TaskCompletionSource open = new(TaskCreationOptions.RunContinuationsAsynchronously);
        private readonly SemaphoreSlim stuck = new(0);
        private readonly SemaphoreSlim finished = new(0);
        private volatile bool stopped;

        /// <summary>One waiter per stream that got stuck in a write.</summary>
        public SemaphoreSlim Stuck => stuck;

        /// <summary>One waiter per stream that finished.</summary>
        public SemaphoreSlim Finished => finished;

        public void Stop() => stopped = true;

        public void Resume()
        {
            stopped = false;
            open.TrySetResult();
        }

        public Action<IApplicationBuilder> Configure(Action<IApplicationBuilder> next) => app =>
        {
            app.Use(async (context, inner) =>
            {
                if (context.Request.Path != StreamPath)
                {
                    await inner();
                    return;
                }
                var body = context.Response.Body;
                context.Response.Body = new HeldStream(body, this);
                try
                {
                    await inner();
                }
                finally
                {
                    context.Response.Body = body;
                    finished.Release();
                }
            });
            next(app);
        };

        private async Task Hold(CancellationToken cancellation)
        {
            if (stopped)
            {
                stuck.Release();
                await open.Task.WaitAsync(cancellation);
            }
        }

        private sealed class HeldStream(Stream inner, StreamProbe probe) : Stream
        {
            public override async ValueTask WriteAsync(ReadOnlyMemory<byte> buffer, CancellationToken cancellationToken = default)
            {
                await probe.Hold(cancellationToken);
                await inner.WriteAsync(buffer, cancellationToken);
            }

            public override Task FlushAsync(CancellationToken cancellationToken) => inner.FlushAsync(cancellationToken);

            public override void Flush() => inner.Flush();

            public override bool CanRead => false;

            public override bool CanSeek => false;

            public override bool CanWrite => true;

            public override long Length => throw new NotSupportedException();

            public override long Position { get => throw new NotSupportedException(); set => throw new NotSupportedException(); }

            public override int Read(byte[] buffer, int offset, int count) => throw new NotSupportedException();

            public override long Seek(long offset, SeekOrigin origin) => throw new NotSupportedException();

            public override void SetLength(long value) => throw new NotSupportedException();

            public override void Write(byte[] buffer, int offset, int count) => throw new NotSupportedException();
        }
    }
}
