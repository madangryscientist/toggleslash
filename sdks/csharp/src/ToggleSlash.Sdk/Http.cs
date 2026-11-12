using System.Globalization;
using System.Net;
using System.Net.Http.Headers;
using System.Text;
using ToggleSlash.Sdk.Evaluation;
using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk;

internal enum FetchKind
{
    /// <summary>A 200 with a ruleset.</summary>
    Ruleset,

    /// <summary>A 304 with a ruleset held.</summary>
    NotModified,

    /// <summary>An error; see <see cref="FetchResult.Error"/>.</summary>
    Failed,

    /// <summary>The stop token fired: <c>close</c> abandoned the request. Nothing is recorded for it.</summary>
    Cancelled,
}

/// <summary>The outcome of one ruleset request.</summary>
internal readonly record struct FetchResult(
    FetchKind Kind,
    Ruleset? Ruleset = null,
    string? Etag = null,
    string? Error = null,
    TimeSpan? RetryAfter = null);

/// <summary>The outcome of one events request, decided by the status alone.</summary>
internal enum EventsOutcome
{
    Sent,
    Keep,
    Drop,
}

internal readonly record struct EventsResult(EventsOutcome Outcome, string? Error, TimeSpan? RetryAfter);

/// <summary>
/// The only code that talks to the network: the ruleset, the stream and the events routes, all on one <see cref="HttpClient"/>
/// (decompression off, cookies off, redirects not followed so a 3xx is <c>http_&lt;status&gt;</c>, the client's own timeout
/// infinite). The headers it adds are <c>Authorization</c>, <c>Accept</c> (stream), <c>If-None-Match</c> and
/// <c>Content-Type</c> (events), plus the <c>Host</c> and <c>Content-Length</c> the HTTP library writes.
/// </summary>
internal sealed class Transport : IDisposable
{
    /// <summary>The longest wait a Retry-After can ask for (contract 7.4).</summary>
    internal static readonly TimeSpan RetryAfterCap = TimeSpan.FromSeconds(300);

    /// <summary>The largest timer span the platform takes (about 49.7 days); a longer limit is clamped to it.</summary>
    internal static readonly TimeSpan MaxTimerSpan = TimeSpan.FromMilliseconds(uint.MaxValue - 2);

    private static readonly UTF8Encoding Utf8 = new(encoderShouldEmitUTF8Identifier: false, throwOnInvalidBytes: false);

    private readonly HttpClient _http;
    private readonly Uri? _ruleset;
    private readonly Uri? _stream;
    private readonly Uri? _events;
    private readonly string _authorization;
    private readonly TimeSpan _timeout;
    private readonly TimeProvider _time;

    internal Transport(string url, string? sdkKey, TimeSpan timeout, TimeProvider time, HttpMessageHandler? handler)
    {
        _timeout = timeout > MaxTimerSpan ? MaxTimerSpan : timeout;
        _time = time;
        _authorization = "Bearer " + sdkKey;
        _ruleset = Build(url, "/sdk/v1/ruleset");
        _stream = Build(url, "/sdk/v1/stream");
        _events = Build(url, "/sdk/v1/events");
        _http = handler is null
            ? new HttpClient(
                new SocketsHttpHandler
                {
                    AllowAutoRedirect = false,
                    UseCookies = false,
                    AutomaticDecompression = DecompressionMethods.None,
                },
                disposeHandler: true)
            : new HttpClient(handler, disposeHandler: false);
        _http.Timeout = Timeout.InfiniteTimeSpan;
    }

    public void Dispose() => _http.Dispose();

    private static Uri? Build(string url, string path) =>
        Uri.TryCreate(url + path, UriKind.Absolute, out var uri) ? uri : null;

    private HttpRequestMessage Request(HttpMethod method, Uri uri)
    {
        var request = new HttpRequestMessage(method, uri) { Version = HttpVersion.Version11 };
        request.Headers.TryAddWithoutValidation("Authorization", _authorization);
        return request;
    }

    /// <summary>Reads Retry-After as 7.4 says: whole decimal seconds only, surrounding spaces ignored, capped at 300 s.</summary>
    internal static TimeSpan? ParseRetryAfter(HttpResponseMessage response)
    {
        return ParseRetryAfter(FirstHeader(response, "Retry-After"));
    }

    internal static TimeSpan? ParseRetryAfter(string? text)
    {
        if (text is null)
        {
            return null;
        }

        var trimmed = text.Trim(' ');
        if (trimmed.Length == 0 || !trimmed.All(static c => c is >= '0' and <= '9'))
        {
            return null;
        }

        // More than ten digits is far over the cap; do not parse (and overflow) it.
        if (trimmed.Length > 10 || !long.TryParse(trimmed, NumberStyles.None, CultureInfo.InvariantCulture, out var seconds))
        {
            return RetryAfterCap;
        }

        var wait = TimeSpan.FromSeconds(Math.Min(seconds, (long)RetryAfterCap.TotalSeconds));
        return wait;
    }

    private static string? FirstHeader(HttpResponseMessage response, string name)
    {
        var headers = response.Headers.NonValidated;
        if (!headers.Contains(name))
        {
            return null;
        }

        foreach (var value in headers[name])
        {
            return value;
        }

        return null;
    }

    /// <summary>One <c>GET /sdk/v1/ruleset</c>. <paramref name="ifNoneMatch"/> is sent only when not null.</summary>
    internal async Task<FetchResult> GetRulesetAsync(string? ifNoneMatch, bool rulesetHeld, CancellationToken stop)
    {
        if (_ruleset is null)
        {
            return new FetchResult(FetchKind.Failed, Error: "network");
        }

        using var timeout = new CancellationTokenSource(_timeout, _time);
        using var linked = CancellationTokenSource.CreateLinkedTokenSource(stop, timeout.Token);
        try
        {
            using var request = Request(HttpMethod.Get, _ruleset);
            if (ifNoneMatch is not null)
            {
                request.Headers.TryAddWithoutValidation("If-None-Match", ifNoneMatch);
            }

            using var response = await _http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, linked.Token).ConfigureAwait(false);
            var status = (int)response.StatusCode;
            if (status == 200)
            {
                var etag = FirstHeader(response, "ETag");
                var bytes = await response.Content.ReadAsByteArrayAsync(linked.Token).ConfigureAwait(false);
                var root = JsonValue.Parse(Utf8.GetString(bytes));
                if (root is JsonObject obj
                    && obj.TryGetValue("flags", out var flags) && flags is JsonArray
                    && obj.TryGetValue("segments", out var segments) && segments is JsonArray)
                {
                    return new FetchResult(FetchKind.Ruleset, Ruleset.Load(root), string.IsNullOrEmpty(etag) ? null : etag);
                }

                return new FetchResult(FetchKind.Failed, Error: "bad_ruleset");
            }

            if (status == 304)
            {
                return rulesetHeld
                    ? new FetchResult(FetchKind.NotModified)
                    : new FetchResult(FetchKind.Failed, Error: "http_304");
            }

            return new FetchResult(
                FetchKind.Failed,
                Error: "http_" + status.ToString(CultureInfo.InvariantCulture),
                RetryAfter: status == 429 ? ParseRetryAfter(response) : null);
        }
        catch (Exception e) when (IsCancellation(e))
        {
            return stop.IsCancellationRequested
                ? new FetchResult(FetchKind.Cancelled)
                : new FetchResult(FetchKind.Failed, Error: timeout.IsCancellationRequested ? "timeout" : "network");
        }
        catch (Exception)
        {
            return stop.IsCancellationRequested
                ? new FetchResult(FetchKind.Cancelled)
                : new FetchResult(FetchKind.Failed, Error: "network");
        }
    }

    private static bool IsCancellation(Exception e) =>
        e is OperationCanceledException || (e is HttpRequestException or IOException && e.InnerException is OperationCanceledException);

    /// <summary>One <c>POST /sdk/v1/events</c>, bounded by the request timeout; only the status decides.</summary>
    internal async Task<EventsResult> PostEventsAsync(string json)
    {
        if (_events is null)
        {
            return new EventsResult(EventsOutcome.Keep, "network", null);
        }

        using var timeout = new CancellationTokenSource(_timeout, _time);
        try
        {
            using var request = Request(HttpMethod.Post, _events);
            var content = new ByteArrayContent(Utf8.GetBytes(json));
            content.Headers.ContentType = new MediaTypeHeaderValue("application/json");
            request.Content = content;
            using var response = await _http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, timeout.Token).ConfigureAwait(false);
            var status = (int)response.StatusCode;
            if (status is >= 200 and < 300)
            {
                return new EventsResult(EventsOutcome.Sent, null, null);
            }

            var error = "http_" + status.ToString(CultureInfo.InvariantCulture);
            if (status == 429)
            {
                return new EventsResult(EventsOutcome.Keep, error, ParseRetryAfter(response));
            }

            return status is >= 500 and < 600 or 408
                ? new EventsResult(EventsOutcome.Keep, error, null)
                : new EventsResult(EventsOutcome.Drop, error, null);
        }
        catch (Exception e) when (IsCancellation(e))
        {
            return new EventsResult(EventsOutcome.Keep, timeout.IsCancellationRequested ? "timeout" : "network", null);
        }
        catch (Exception)
        {
            return new EventsResult(EventsOutcome.Keep, "network", null);
        }
    }

    /// <summary>
    /// Opens <c>GET /sdk/v1/stream</c>. The open is bounded by the request timeout, from the request until the response
    /// headers; after that only the reads' own tokens apply.
    /// </summary>
    internal async Task<StreamConnection> OpenStreamAsync(TimeSpan watchdog, CancellationToken stop)
    {
        if (_stream is null)
        {
            return StreamConnection.Failed("network", null);
        }

        using var timeout = new CancellationTokenSource(_timeout, _time);
        using var linked = CancellationTokenSource.CreateLinkedTokenSource(stop, timeout.Token);
        HttpRequestMessage? request = null;
        HttpResponseMessage? response = null;
        try
        {
            request = Request(HttpMethod.Get, _stream);
            request.Headers.TryAddWithoutValidation("Accept", "text/event-stream");
            response = await _http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, linked.Token).ConfigureAwait(false);
            var status = (int)response.StatusCode;
            if (status != 200)
            {
                var retry = status == 429 ? ParseRetryAfter(response) : null;
                response.Dispose();
                request.Dispose();
                return StreamConnection.Failed("http_" + status.ToString(CultureInfo.InvariantCulture), retry);
            }

            // The open is over: the open limit must not fire on the body.
            timeout.CancelAfter(Timeout.InfiniteTimeSpan);
            var body = await response.Content.ReadAsStreamAsync(stop).ConfigureAwait(false);
            return new StreamConnection(request, response, body, watchdog, _time, stop);
        }
        catch (Exception e)
        {
            response?.Dispose();
            request?.Dispose();
            stop.ThrowIfCancellationRequested();
            return StreamConnection.Failed(IsCancellation(e) && timeout.IsCancellationRequested ? "timeout" : "network", null);
        }
    }
}

/// <summary>An open (or failed-to-open) event stream. It is read in pieces, each under the watchdog.</summary>
internal sealed class StreamConnection : IDisposable
{
    private readonly HttpRequestMessage? _request;
    private readonly HttpResponseMessage? _response;
    private readonly Stream? _body;
    private readonly CancellationTokenSource? _watchdogSource;
    private readonly CancellationTokenSource? _linked;
    private readonly TimeSpan _watchdog;
    private readonly CancellationToken _stop;

    internal StreamConnection(HttpRequestMessage request, HttpResponseMessage response, Stream body, TimeSpan watchdog, TimeProvider time, CancellationToken stop)
    {
        _request = request;
        _response = response;
        _body = body;
        _watchdog = watchdog > Transport.MaxTimerSpan ? Transport.MaxTimerSpan : watchdog;
        _stop = stop;
        _watchdogSource = new CancellationTokenSource(Timeout.InfiniteTimeSpan, time);
        _linked = CancellationTokenSource.CreateLinkedTokenSource(stop, _watchdogSource.Token);
    }

    private StreamConnection(string error, TimeSpan? retryAfter)
    {
        Error = error;
        RetryAfter = retryAfter;
    }

    /// <summary>The failure to open, or null when the stream is open.</summary>
    internal string? Error { get; }

    /// <summary>The Retry-After of a 429 answer.</summary>
    internal TimeSpan? RetryAfter { get; }

    internal static StreamConnection Failed(string error, TimeSpan? retryAfter) => new(error, retryAfter);

    /// <summary>
    /// Reads the next piece. The watchdog is re-armed before each read, so any bytes restart it. Returns the byte count
    /// (0 is a clean end) and, on a failure, <c>timeout</c> (the watchdog fired) or <c>network</c>. A stop (close) throws
    /// <see cref="OperationCanceledException"/>.
    /// </summary>
    internal async Task<(int Count, string? Error)> ReadAsync(byte[] buffer)
    {
        _watchdogSource!.CancelAfter(_watchdog);
        try
        {
            var count = await _body!.ReadAsync(buffer, _linked!.Token).ConfigureAwait(false);
            return (count, null);
        }
        catch (Exception)
        {
            _stop.ThrowIfCancellationRequested();
            return (0, _watchdogSource.IsCancellationRequested ? "timeout" : "network");
        }
    }

    public void Dispose()
    {
        _linked?.Dispose();
        _watchdogSource?.Dispose();
        _body?.Dispose();
        _response?.Dispose();
        _request?.Dispose();
    }
}
