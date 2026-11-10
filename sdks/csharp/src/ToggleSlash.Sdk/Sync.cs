using System.Text.Json;
using ToggleSlash.Sdk.Evaluation;
using ToggleSlash.Sdk.Sse;

namespace ToggleSlash.Sdk;

/// <summary>Everything a reader needs, in one immutable value that is replaced whole.</summary>
internal sealed record SyncState(Ruleset? Ruleset, string? Etag, DateTimeOffset? LastSyncAt, StreamState Stream, string? LastError);

/// <summary>
/// The ruleset, its ETag and the status fields. Readers take no lock: they read one reference. Writers take a
/// lock held only for the swap itself, never across I/O, a callback or an await.
/// </summary>
internal sealed class StateHolder(SyncState initial)
{
    private readonly Lock _gate = new();
    private volatile SyncState _state = initial;

    public SyncState Current => _state;

    public void Update(Func<SyncState, SyncState> change)
    {
        lock (_gate) _state = change(_state);
    }

    /// <summary>A stream state, unless the client is closed: closed is final.</summary>
    public void SetStream(StreamState stream) =>
        Update(s => s.Stream == StreamState.Closed ? s : s with { Stream = stream });

    public void Close() => Update(s => s with { Stream = StreamState.Closed });
}

internal sealed class SyncOptions
{
    public required string BaseUrl { get; init; }
    public required string SdkKey { get; init; }
    public required HttpClient Http { get; init; }
    public required TimeProvider Time { get; init; }
    public required Func<double> Jitter { get; init; }
    public required bool Stream { get; init; }
    public required TimeSpan PollInterval { get; init; }
    public required TimeSpan RequestTimeout { get; init; }
    public required TimeSpan StreamIdle { get; init; }
    public required Action<Exception> OnError { get; init; }
}

/// <summary>
/// Keeps one ruleset current: it fetches with <c>If-None-Match</c>, follows the stream when it can and polls
/// when it cannot. Nothing in here throws; every failure goes to <c>onError</c> and leaves the last good
/// ruleset in place.
/// </summary>
internal sealed class RulesetSync(SyncOptions options, StateHolder state, CancellationToken lifetime)
{
    private static readonly TimeSpan BackoffBase = TimeSpan.FromSeconds(1);

    // The server's 429 asked for nothing to reach it before this time. Only the sync loop reads or writes it.
    private DateTimeOffset _retryAt = DateTimeOffset.MinValue;

    // Whether the stream being followed has announced itself. Also loop-only.
    private bool _connected;

    /// <summary>Loads once, then follows until the lifetime token fires.</summary>
    public async Task RunAsync(Action onFirstLoad)
    {
        await RefreshAsync().ConfigureAwait(false);
        onFirstLoad();
        var failures = 0;
        while (!lifetime.IsCancellationRequested)
        {
            // A 429 on the ruleset says wait, and without this the stream would be asked straight after it.
            if (options.Stream) await SleepAsync(TimeSpan.Zero).ConfigureAwait(false);
            if (lifetime.IsCancellationRequested) return;
            var connected = options.Stream && await FollowAsync().ConfigureAwait(false);
            failures = connected ? 0 : failures + 1;
            await SleepAsync(Delay(failures)).ConfigureAwait(false);
            // Sleep first: the ruleset was only just loaded, or the stream only just failed. A stream that was
            // up needs no poll, since reconnecting announces the tag and catches up on its own.
            if (!connected && !lifetime.IsCancellationRequested) await RefreshAsync().ConfigureAwait(false);
        }
    }

    private async Task SleepAsync(TimeSpan wait)
    {
        var held = _retryAt - options.Time.GetUtcNow();
        if (held > wait) wait = held;
        if (wait <= TimeSpan.Zero) return;
        try
        {
            await Task.Delay(wait, options.Time, lifetime).ConfigureAwait(false);
        }
        catch (OperationCanceledException)
        {
            // Closing.
        }
    }

    /// <summary>Exponential from a second up to the poll interval, jittered; with no stream, just the interval.</summary>
    internal TimeSpan Delay(int failures)
    {
        if (!options.Stream) return options.PollInterval;
        var ceiling = BackoffBase * Math.Pow(2, Math.Min(failures, 16));
        if (ceiling > options.PollInterval) ceiling = options.PollInterval;
        return ceiling * (0.5 + (options.Jitter() / 2));
    }

    private void HonourRetryAfter(HttpResponseMessage response)
    {
        if (Http.RetryAfter(response) is not { } wait) return;
        var until = options.Time.GetUtcNow() + wait;
        if (until > _retryAt) _retryAt = until;
    }

    private void Synced() =>
        state.Update(s => s with { LastSyncAt = options.Time.GetUtcNow(), LastError = null });

    private void Failed(Exception error, Deadline? deadline)
    {
        if (lifetime.IsCancellationRequested) return;
        var code = Classify(error, deadline);
        state.Update(s => s with { LastError = code });
        options.OnError(error);
    }

    private static string Classify(Exception error, Deadline? deadline) =>
        error switch
        {
            _ when deadline is { TimedOut: true } => LastErrorCodes.Timeout,
            TimeoutException => LastErrorCodes.Timeout,
            HttpStatusException status => "http_" + status.Status,
            BadRulesetException or JsonException => LastErrorCodes.BadRuleset,
            _ => LastErrorCodes.Network,
        };

    /// <summary>One conditional fetch of the ruleset.</summary>
    public async Task<bool> RefreshAsync()
    {
        Deadline? deadline = null;
        try
        {
            deadline = new Deadline(options.RequestTimeout, options.Time, lifetime);
            var held = state.Current;
            using var request = Http.Request(HttpMethod.Get, options.BaseUrl + "/sdk/v1/ruleset", options.SdkKey, "application/json");
            if (held is { Etag: { } heldTag, Ruleset: not null }) request.Headers.TryAddWithoutValidation("If-None-Match", heldTag);
            using var response = await options.Http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, deadline.Token).ConfigureAwait(false);
            if ((int)response.StatusCode == 304)
            {
                Synced();
                return true;
            }
            if ((int)response.StatusCode != 200)
            {
                HonourRetryAfter(response);
                throw new HttpStatusException(response.StatusCode, $"GET /sdk/v1/ruleset answered {(int)response.StatusCode}");
            }
            var body = await response.Content.ReadAsByteArrayAsync(deadline.Token).ConfigureAwait(false);
            using var document = JsonText.Parse(body);
            if (!JsonText.HasRulesetShape(document.RootElement)) throw new BadRulesetException("GET /sdk/v1/ruleset sent no flags and segments");
            var ruleset = Ruleset.Load(document.RootElement);
            var etag = Http.ETag(response);
            state.Update(s => s with { Ruleset = ruleset, Etag = etag, LastSyncAt = options.Time.GetUtcNow(), LastError = null });
            return true;
        }
        catch (Exception error)
        {
            Failed(error, deadline);
            return false;
        }
        finally
        {
            deadline?.Dispose();
        }
    }

    /// <summary>
    /// Follows <c>GET /sdk/v1/stream</c> until it ends or fails. True if it connected, so the caller can tell a
    /// stream that was up from one that never was.
    /// </summary>
    private async Task<bool> FollowAsync()
    {
        _connected = false;
        Deadline? deadline = null;
        state.SetStream(StreamState.Connecting);
        try
        {
            deadline = new Deadline(options.RequestTimeout, options.Time, lifetime);
            using var request = Http.Request(HttpMethod.Get, options.BaseUrl + "/sdk/v1/stream", options.SdkKey, "text/event-stream");
            using var response = await options.Http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, deadline.Token).ConfigureAwait(false);
            if ((int)response.StatusCode != 200)
            {
                HonourRetryAfter(response);
                throw new HttpStatusException(response.StatusCode, $"GET /sdk/v1/stream answered {(int)response.StatusCode}");
            }
            deadline.Dispose();
            deadline = null;
            await using var body = await response.Content.ReadAsStreamAsync(lifetime).ConfigureAwait(false);
            await PumpAsync(body).ConfigureAwait(false);
        }
        catch (Exception error)
        {
            Failed(error, deadline);
        }
        finally
        {
            deadline?.Dispose();
            state.SetStream(StreamState.Disconnected);
        }
        return _connected;
    }

    /// <summary>
    /// Reads events until the stream ends. Each read has the idle limit to itself, so a keep-alive restarts it
    /// and a stream that goes quiet is dropped as a timeout.
    /// </summary>
    private async Task PumpAsync(Stream body)
    {
        var parser = new SseParser();
        var buffer = new byte[8192];
        for (; ; )
        {
            int read;
            using (var idle = new Deadline(options.StreamIdle, options.Time, lifetime))
            {
                try
                {
                    read = await body.ReadAsync(buffer, idle.Token).ConfigureAwait(false);
                }
                catch (Exception error) when (idle.TimedOut)
                {
                    throw new TimeoutException("the stream went quiet", error);
                }
            }
            if (read == 0) return;
            foreach (var sse in parser.Feed(buffer.AsSpan(0, read)))
            {
                if (sse.Name is not ("changed" or "connected")) continue;
                if (sse.Name == "connected")
                {
                    _connected = true;
                    state.SetStream(StreamState.Connected);
                    state.Update(s => s with { LastError = null });
                }
                if (!await OnTagAsync(sse.Data).ConfigureAwait(false))
                {
                    // The fetch the stream asked for failed, and a stream that stays up would never retry it.
                    // End it and count it as not connected, so the backoff and the poll take over.
                    _connected = false;
                    return;
                }
            }
        }
    }

    /// <summary>The stream names the ruleset's ETag; fetch only when it is not the one held.</summary>
    private async Task<bool> OnTagAsync(string tag)
    {
        var held = state.Current;
        if (held.Ruleset is not null && tag == held.Etag)
        {
            Synced();
            return true;
        }
        return await RefreshAsync().ConfigureAwait(false);
    }
}
