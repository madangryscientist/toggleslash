using System.Globalization;
using System.Net;
using System.Net.Http.Headers;
using System.Text.Json;

namespace ToggleSlash.Sdk;

/// <summary>One line of <c>POST /sdk/v1/events</c>: counts only, never a context or anything about an end user.</summary>
internal readonly record struct UsageEntry(string Flag, int VariationIndex, long Count, string Day);

/// <summary>
/// Counts how often each flag variation was served, per UTC day, and sends the totals in batches. Recording
/// is a dictionary add under a lock that is never held across I/O, so it is safe on a request path. A batch
/// that fails for a reason that can pass (the server is down or busy) is put back; one the server refused is
/// dropped, since it would be refused again. After a 429 with a <c>Retry-After</c>, nothing is sent until
/// that has passed.
/// </summary>
internal sealed class UsageReporter(
    string baseUrl,
    string sdkKey,
    HttpClient http,
    TimeProvider time,
    TimeSpan requestTimeout,
    Action<Exception> onError) : IDisposable
{
    /// <summary>
    /// The server accepts 1000 events and 128 KiB a request; an entry is at most about 140 bytes, so 500
    /// stay under both.
    /// </summary>
    internal const int MaxBatch = 500;

    private readonly Lock _gate = new();
    private readonly SemaphoreSlim _flushing = new(1, 1);
    private Dictionary<(string Flag, int Index, string Day), long> _pending = [];

    // The server's 429 asked for nothing to reach it before this time. Read and written under _flushing.
    private DateTimeOffset _retryAt = DateTimeOffset.MinValue;

    public void Dispose() => _flushing.Dispose();

    public void Record(string flag, int variationIndex)
    {
        var day = time.GetUtcNow().ToString("yyyy-MM-dd", CultureInfo.InvariantCulture);
        // JSON text cannot hold a lone surrogate, and one entry the body cannot be written for would be kept
        // and fail for ever, taking every later entry with it.
        flag = WellFormed(flag);
        Add(new UsageEntry(flag, variationIndex, 1, day));
    }

    private static string WellFormed(string text)
    {
        for (var i = 0; i < text.Length; i++)
        {
            if (char.IsHighSurrogate(text[i]) && i + 1 < text.Length && char.IsLowSurrogate(text[i + 1])) i++;
            else if (char.IsSurrogate(text[i])) return string.Concat(text.EnumerateRunes().Select(r => r.ToString()));
        }
        return text;
    }

    private void Add(UsageEntry entry)
    {
        var key = (entry.Flag, entry.VariationIndex, entry.Day);
        lock (_gate)
        {
            _pending[key] = _pending.GetValueOrDefault(key) + entry.Count;
        }
    }

    internal int PendingCount
    {
        get
        {
            lock (_gate) return _pending.Count;
        }
    }

    private bool Holding() => time.GetUtcNow() < _retryAt;

    /// <summary>Sends what is pending. It never throws; a call during a send waits for that send first.</summary>
    public async Task FlushAsync()
    {
        await _flushing.WaitAsync().ConfigureAwait(false);
        try
        {
            await SendAsync().ConfigureAwait(false);
        }
        catch (Exception error)
        {
            onError(error);
        }
        finally
        {
            _flushing.Release();
        }
    }

    /// <summary>
    /// The last flush, on closing. It will not wait out a <c>Retry-After</c>, and it will not ignore one, so
    /// counts still held back then are lost, and that is reported.
    /// </summary>
    public async Task FinishAsync()
    {
        await FlushAsync().ConfigureAwait(false);
        await _flushing.WaitAsync().ConfigureAwait(false);
        int unsent;
        try
        {
            unsent = Holding() ? PendingCount : 0;
        }
        finally
        {
            _flushing.Release();
        }
        if (unsent > 0) onError(new InvalidOperationException($"closed with {unsent} usage counts unsent: the server asked to wait"));
    }

    private async Task SendAsync()
    {
        if (Holding()) return;
        List<UsageEntry> entries;
        lock (_gate)
        {
            entries = [.. _pending.Select(p => new UsageEntry(p.Key.Flag, p.Key.Index, p.Value, p.Key.Day))];
            _pending = [];
        }
        for (var start = 0; start < entries.Count; start += MaxBatch)
        {
            var batch = entries.GetRange(start, Math.Min(MaxBatch, entries.Count - start));
            if (await PostAsync(batch).ConfigureAwait(false)) continue;
            foreach (var entry in entries.Skip(start)) Add(entry);
            return;
        }
    }

    /// <summary>What to do with a batch the server did not take: try it again later, or give up on it.</summary>
    private static bool Retryable(int status) => status >= 500 || status is 408 or 429;

    /// <summary>False when the batch should be kept for another try.</summary>
    private async Task<bool> PostAsync(List<UsageEntry> batch)
    {
        using var deadline = new Deadline(requestTimeout, time, CancellationToken.None);
        try
        {
            using var request = Http.Request(HttpMethod.Post, baseUrl + "/sdk/v1/events", sdkKey, "application/json");
            request.Content = new ByteArrayContent(Body(batch));
            request.Content.Headers.ContentType = new MediaTypeHeaderValue("application/json");
            using var response = await http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, deadline.Token).ConfigureAwait(false);
            var status = (int)response.StatusCode;
            if (status is >= 200 and < 300) return true;
            if (Http.RetryAfter(response) is { } wait)
            {
                var until = time.GetUtcNow() + wait;
                if (until > _retryAt) _retryAt = until;
            }
            onError(new HttpStatusException((HttpStatusCode)status, $"POST /sdk/v1/events answered {status}"));
            return !Retryable(status);
        }
        catch (Exception error)
        {
            onError(deadline.TimedOut ? new TimeoutException("POST /sdk/v1/events did not answer in time", error) : error);
            return false;
        }
    }

    private static byte[] Body(List<UsageEntry> batch)
    {
        using var stream = new MemoryStream();
        using (var writer = new Utf8JsonWriter(stream))
        {
            writer.WriteStartObject();
            writer.WriteStartArray("events");
            foreach (var entry in batch)
            {
                writer.WriteStartObject();
                writer.WriteString("flag", entry.Flag);
                writer.WriteNumber("variationIndex", entry.VariationIndex);
                writer.WriteNumber("count", entry.Count);
                writer.WriteString("day", entry.Day);
                writer.WriteEndObject();
            }
            writer.WriteEndArray();
            writer.WriteEndObject();
        }
        return stream.ToArray();
    }
}
