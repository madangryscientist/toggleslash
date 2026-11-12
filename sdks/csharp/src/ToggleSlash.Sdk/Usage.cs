using System.Globalization;
using System.Text;
using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk;

public sealed partial class ToggleSlashClient
{
    private const int MaxEntriesPerRequest = 500;

    private readonly object _usageLock = new();
    private readonly SemaphoreSlim _flushGate = new(1, 1);
    private Dictionary<(string Flag, int Index, string Day), long> _counts = [];

    // The Retry-After hold of the usage reports (separate from the ruleset's).
    private long _usageHoldTicks;

    /// <summary>When the usage hold ends (a test reads it to see the cap).</summary>
    internal DateTimeOffset UsageHoldUntil => new(Interlocked.Read(ref _usageHoldTicks), TimeSpan.Zero);

    /// <summary>Counts one served evaluation: an in-memory add under a lock that is never held across the network.</summary>
    private void Record(string flagKey, int index)
    {
        var key = (flagKey, index, Day(_time.GetUtcNow()));
        lock (_usageLock)
        {
            if (_closing)
            {
                return;
            }

            _counts[key] = _counts.GetValueOrDefault(key) + 1;
        }
    }

    private Dictionary<(string Flag, int Index, string Day), long> TakeCounts()
    {
        lock (_usageLock)
        {
            var taken = _counts;
            _counts = [];
            return taken;
        }
    }

    private void Merge(IEnumerable<KeyValuePair<(string Flag, int Index, string Day), long>> kept)
    {
        lock (_usageLock)
        {
            foreach (var (key, count) in kept)
            {
                _counts[key] = _counts.GetValueOrDefault(key) + count;
            }
        }
    }

    private bool UsageHeld() => Interlocked.Read(ref _usageHoldTicks) > _time.GetUtcNow().UtcTicks;

    private async Task FlushLoopAsync()
    {
        var stop = _flushStop.Token;
        try
        {
            while (await DelayAsync(_flushInterval, stop).ConfigureAwait(false))
            {
                await FlushAsync(final: false).ConfigureAwait(false);
            }
        }
        catch (OperationCanceledException)
        {
            // close stopped the loop.
        }
        catch (Exception e)
        {
            if (!_closing)
            {
                ReportError(e);
            }
        }
    }

    private Task FinalFlushAsync() => FlushAsync(final: true);

    /// <summary>Flushes the pending counts (9). Flushes are serial. The final one does not wait out a hold.</summary>
    internal async Task FlushAsync(bool final)
    {
        await _flushGate.WaitAsync().ConfigureAwait(false);
        try
        {
            if (UsageHeld())
            {
                if (final)
                {
                    var lost = TakeCounts().Count;
                    if (lost > 0)
                    {
                        ReportError(new ToggleSlashException(
                            "closed with " + lost.ToString(CultureInfo.InvariantCulture) + " usage counts unsent: the server asked to wait",
                            "usage_unsent"));
                    }
                }

                return;
            }

            var pending = TakeCounts();
            if (pending.Count == 0)
            {
                return;
            }

            var entries = pending.ToArray();
            for (var start = 0; start < entries.Length; start += MaxEntriesPerRequest)
            {
                var chunk = entries.AsMemory(start, Math.Min(MaxEntriesPerRequest, entries.Length - start));
                var result = await _transport.PostEventsAsync(Body(chunk.Span)).ConfigureAwait(false);
                if (result.Outcome == EventsOutcome.Sent)
                {
                    continue;
                }

                if (result.RetryAfter is { } wait)
                {
                    Lengthen(ref _usageHoldTicks, _time.GetUtcNow() + wait);
                }

                var code = result.Error!;
                if (result.Outcome == EventsOutcome.Keep)
                {
                    // This request's entries and the untried rest go back; the flush stops.
                    Merge(entries.Skip(start));
                    ReportError(new ToggleSlashException("the usage request failed: " + code, code));
                    return;
                }

                ReportError(new ToggleSlashException("the usage request failed: " + code, code));
            }
        }
        finally
        {
            _flushGate.Release();
        }
    }

    /// <summary>The events body, written by hand so the four fields are exactly those and nothing else.</summary>
    private static string Body(ReadOnlySpan<KeyValuePair<(string Flag, int Index, string Day), long>> entries)
    {
        var text = new StringBuilder("{\"events\":[");
        for (var i = 0; i < entries.Length; i++)
        {
            var ((flag, index, day), count) = entries[i];
            if (i > 0)
            {
                text.Append(',');
            }

            text.Append("{\"flag\":");
            JsonValue.WriteString(text, flag);
            text.Append(",\"variationIndex\":").Append(index.ToString(CultureInfo.InvariantCulture));
            text.Append(",\"count\":").Append(count.ToString(CultureInfo.InvariantCulture));
            text.Append(",\"day\":");
            JsonValue.WriteString(text, day);
            text.Append('}');
        }

        return text.Append("]}").ToString();
    }
}
