using System.Globalization;
using System.Text;
using ToggleSlash.Sdk.Sse;

namespace ToggleSlash.Sdk;

public sealed partial class ToggleSlashClient
{
    // The Retry-After hold shared by the ruleset and the stream (7.4): a point in time (UTC ticks), only ever lengthened.
    private long _syncHoldTicks;

    /// <summary>When the ruleset and stream hold ends (a test reads it to see the cap).</summary>
    internal DateTimeOffset SyncHoldUntil => new(Interlocked.Read(ref _syncHoldTicks), TimeSpan.Zero);

    private async Task SyncLoopAsync()
    {
        var stop = _stop.Token;
        try
        {
            // The first load: one ruleset request, no timer, no hold. Its end settles ready.
            await PollAsync(first: true, stop).ConfigureAwait(false);
            SettleReady();

            var failures = 0;
            while (!stop.IsCancellationRequested)
            {
                if (_stream && !await WaitHoldAsync(stop).ConfigureAwait(false))
                {
                    break;
                }

                var connected = _stream && await FollowStreamAsync(stop).ConfigureAwait(false);
                failures = connected ? 0 : failures + 1;
                if (!await DelayAsync(Backoff(failures), stop).ConfigureAwait(false) || !await WaitHoldAsync(stop).ConfigureAwait(false))
                {
                    break;
                }

                if (!connected)
                {
                    await PollAsync(first: false, stop).ConfigureAwait(false);
                }
            }
        }
        catch (OperationCanceledException)
        {
            // close abandoned a request or a wait.
        }
        catch (Exception e)
        {
            // A bug in the loop must not end the client silently or escape: report it, and let the loop end.
            if (!_closing)
            {
                ReportError(e);
            }
        }
        finally
        {
            if (_closing)
            {
                _ready.TrySetResult(false);
            }
            else
            {
                SettleReady();
            }
        }
    }

    /// <summary>The wait after a loop turn (7.3): <c>pollInterval</c> with the stream off, else a jittered, capped backoff.</summary>
    private TimeSpan Backoff(int failures)
    {
        if (!_stream)
        {
            return _pollInterval;
        }

        var seconds = Math.Min(_pollInterval.TotalSeconds, Math.Pow(2, Math.Min(failures, 16)));
        var draw = _jitter();
        if (!(draw >= 0) || draw >= 1)
        {
            draw = 0;
        }

        return TimeSpan.FromSeconds(seconds * (0.5 + (0.5 * draw)));
    }

    /// <summary>Waits <paramref name="span"/> on the client's clock; false when close cut it short.</summary>
    private async Task<bool> DelayAsync(TimeSpan span, CancellationToken stop)
    {
        try
        {
            while (span > TimeSpan.Zero)
            {
                var piece = span > Transport.MaxTimerSpan ? Transport.MaxTimerSpan : span;
                await Task.Delay(piece, _time, stop).ConfigureAwait(false);
                span -= piece;
            }

            return !stop.IsCancellationRequested;
        }
        catch (OperationCanceledException)
        {
            return false;
        }
    }

    /// <summary>Waits until the hold, a time and not a sleep, has passed.</summary>
    private async Task<bool> WaitHoldAsync(CancellationToken stop)
    {
        while (true)
        {
            var until = new DateTimeOffset(Interlocked.Read(ref _syncHoldTicks), TimeSpan.Zero);
            var remaining = until - _time.GetUtcNow();
            if (remaining <= TimeSpan.Zero)
            {
                return !stop.IsCancellationRequested;
            }

            if (!await DelayAsync(remaining, stop).ConfigureAwait(false))
            {
                return false;
            }
        }
    }

    private static void Lengthen(ref long ticks, DateTimeOffset until)
    {
        var wanted = until.UtcTicks;
        while (true)
        {
            var current = Interlocked.Read(ref ticks);
            if (wanted <= current || Interlocked.CompareExchange(ref ticks, wanted, current) == current)
            {
                return;
            }
        }
    }

    private void HoldSync(TimeSpan? retryAfter)
    {
        if (retryAfter is { } wait)
        {
            Lengthen(ref _syncHoldTicks, _time.GetUtcNow() + wait);
        }
    }

    // ---- the ruleset request --------------------------------------------------------------------------------

    /// <summary>One ruleset request, applied to the state. True when it was a sync.</summary>
    private async Task<bool> PollAsync(bool first, CancellationToken stop, bool markDisconnected = false)
    {
        _ = first;
        var held = Volatile.Read(ref _state);
        var tag = held.Ruleset is not null && !string.IsNullOrEmpty(held.Etag) ? held.Etag : null;
        var result = await _transport.GetRulesetAsync(tag, held.Ruleset is not null, stop).ConfigureAwait(false);
        return Apply(result, markDisconnected);
    }

    private bool Apply(FetchResult result, bool markDisconnected)
    {
        switch (result.Kind)
        {
            case FetchKind.Ruleset:
                {
                    var now = Now();
                    Update(s => s with { Ruleset = result.Ruleset, Etag = result.Etag, LastSyncAt = now, LastError = null });
                    return true;
                }

            case FetchKind.NotModified:
                {
                    var now = Now();
                    Update(s => s with { LastSyncAt = now, LastError = null });
                    return true;
                }

            case FetchKind.Failed:
                Fail(result.Error!, result.RetryAfter, markDisconnected);
                return false;

            default:
                return false;
        }
    }

    /// <summary>Records a failed ruleset or stream attempt: state first (one swap), then <c>OnError</c>. Nothing after close.</summary>
    private void Fail(string code, TimeSpan? retryAfter, bool markDisconnected)
    {
        if (_closing)
        {
            return;
        }

        HoldSync(retryAfter);
        Update(s => s with { LastError = code, Stream = markDisconnected && s.Stream != StreamState.off ? StreamState.disconnected : s.Stream });
        ReportError(new ToggleSlashException("the " + (markDisconnected ? "stream" : "ruleset") + " request failed: " + code, code));
    }

    // ---- the stream -----------------------------------------------------------------------------------------

    /// <summary>
    /// Opens the stream and follows it until it ends (7.2). True only if its <c>connected</c> event arrived and the stream was
    /// not ended by a failed fetch.
    /// </summary>
    private async Task<bool> FollowStreamAsync(CancellationToken stop)
    {
        Update(s => s with { Stream = StreamState.connecting });
        using var connection = await _transport.OpenStreamAsync(_watchdog, stop).ConfigureAwait(false);
        if (connection.Error is { } openError)
        {
            Fail(openError, connection.RetryAfter, markDisconnected: true);
            return false;
        }

        var parser = new SseParser();
        var decoder = new UTF8Encoding(false, false).GetDecoder();
        var buffer = new byte[16 * 1024];
        var chars = new char[buffer.Length + 4];
        var events = new List<SseEvent>();
        var connected = false;
        while (true)
        {
            var (count, error) = await connection.ReadAsync(buffer).ConfigureAwait(false);
            if (error is not null)
            {
                Fail(error, null, markDisconnected: true);
                return connected;
            }

            if (count == 0)
            {
                // A clean end leaves lastError alone.
                Update(s => s with { Stream = StreamState.disconnected });
                return connected;
            }

            var length = decoder.GetChars(buffer, 0, count, chars, 0, flush: false);
            events.Clear();
            parser.Feed(chars.AsSpan(0, length), events);
            foreach (var evt in events)
            {
                if (evt.Name is not ("connected" or "changed"))
                {
                    continue;
                }

                if (evt.Name == "connected")
                {
                    connected = true;
                    Update(s => s with { Stream = StreamState.connected, LastError = null });
                }

                if (!await OnTagAsync(evt.Data, stop).ConfigureAwait(false))
                {
                    return false;
                }
            }
        }
    }

    /// <summary>The event's tag: a sync when it is the ETag held, else a ruleset request. False when that request failed.</summary>
    private async Task<bool> OnTagAsync(string tag, CancellationToken stop)
    {
        var held = Volatile.Read(ref _state);
        if (held.Ruleset is not null && !string.IsNullOrEmpty(held.Etag) && string.Equals(tag, held.Etag, StringComparison.Ordinal))
        {
            var now = Now();
            Update(s => s with { LastSyncAt = now, LastError = null });
            return true;
        }

        return await PollAsync(first: false, stop, markDisconnected: true).ConfigureAwait(false);
    }

    internal static string Day(DateTimeOffset now) => now.UtcDateTime.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture);
}
