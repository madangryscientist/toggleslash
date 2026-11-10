using System.Globalization;
using System.Net;

namespace ToggleSlash.Sdk;

/// <summary>The server answered a status other than the expected one.</summary>
internal sealed class HttpStatusException(HttpStatusCode status, string message) : Exception(message)
{
    public int Status { get; } = (int)status;
}

/// <summary>A <c>200</c> whose body is not a ruleset.</summary>
internal sealed class BadRulesetException(string message) : Exception(message);

/// <summary>
/// A token that fires when the client closes or after <c>limit</c>, whichever is first, on the client's clock.
/// <see cref="TimedOut"/> tells the second from the first, so a failure caused by <c>close</c> stays silent.
/// </summary>
internal sealed class Deadline : IDisposable
{
    private readonly CancellationTokenSource _timeout;
    private readonly CancellationTokenSource _linked;
    private readonly CancellationToken _parent;

    public Deadline(TimeSpan limit, TimeProvider time, CancellationToken parent)
    {
        _parent = parent;
        _timeout = new CancellationTokenSource(limit, time);
        _linked = CancellationTokenSource.CreateLinkedTokenSource(parent, _timeout.Token);
    }

    public CancellationToken Token => _linked.Token;

    /// <summary>True when the time ran out and the client was not closing.</summary>
    public bool TimedOut => _timeout.IsCancellationRequested && !_parent.IsCancellationRequested;

    public void Dispose()
    {
        _linked.Dispose();
        _timeout.Dispose();
    }
}

internal static class Http
{
    /// <summary>However long a server asks, a client is back within this.</summary>
    private static readonly TimeSpan MaxRetryAfter = TimeSpan.FromMinutes(5);

    /// <summary>
    /// How long a 429 asks to wait, capped. Null for any other answer, and for a <c>Retry-After</c> that is
    /// missing or is not whole decimal seconds (the server never sends a date).
    /// </summary>
    public static TimeSpan? RetryAfter(HttpResponseMessage response)
    {
        if (response.StatusCode != HttpStatusCode.TooManyRequests) return null;
        var headers = response.Headers.NonValidated;
        if (!headers.Contains("Retry-After")) return null;
        var text = headers["Retry-After"].ToString().Trim();
        if (text.Length == 0 || !text.All(char.IsAsciiDigit)) return null;
        if (text.Length > 9) return MaxRetryAfter;
        var seconds = long.Parse(text, NumberStyles.None, CultureInfo.InvariantCulture);
        var wait = TimeSpan.FromSeconds(seconds);
        return wait < MaxRetryAfter ? wait : MaxRetryAfter;
    }

    /// <summary>The ETag as the server sent it, or null.</summary>
    public static string? ETag(HttpResponseMessage response) =>
        response.Headers.NonValidated.Contains("ETag") ? response.Headers.NonValidated["ETag"].ToString() : null;

    /// <summary>One handler for the SDK's whole life: no redirects, no cookies, no proxy surprises beyond the system's.</summary>
    public static HttpClient CreateClient(HttpMessageHandler? handler)
    {
        handler ??= new SocketsHttpHandler
        {
            AllowAutoRedirect = false,
            UseCookies = false,
            ConnectTimeout = TimeSpan.FromSeconds(10),
        };
        return new HttpClient(handler, disposeHandler: true) { Timeout = Timeout.InfiniteTimeSpan };
    }

    public static HttpRequestMessage Request(HttpMethod method, string url, string sdkKey, string accept)
    {
        var request = new HttpRequestMessage(method, url);
        request.Headers.TryAddWithoutValidation("Authorization", "Bearer " + sdkKey);
        request.Headers.TryAddWithoutValidation("Accept", accept);
        return request;
    }
}
