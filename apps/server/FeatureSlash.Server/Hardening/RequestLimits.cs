using FeatureSlash.Server.Auth;
using Microsoft.AspNetCore.Http.Timeouts;

namespace FeatureSlash.Server.Hardening;

/// <summary>
/// How big a request may be and how long the server waits on one. Kestrel enforces the sizes and the connection
/// timeouts; the request timeout is ASP.NET's, so it can answer in the API's own error shape.
/// </summary>
public static class RequestLimits
{
    /// <summary>The largest request body anywhere. <c>POST /sdk/v1/events</c> is stricter and keeps its own 128 KiB.</summary>
    private const long MaxBodyBytes = 1024 * 1024;

    private static readonly TimeSpan RequestTimeout = TimeSpan.FromSeconds(30);

    /// <summary>Time to send the request line and headers. A client that never finishes holds a connection for nothing.</summary>
    private static readonly TimeSpan HeadersTimeout = TimeSpan.FromSeconds(30);

    /// <summary>How long an idle connection waits for its next request. It does not apply while a request, a stream included, is open.</summary>
    private static readonly TimeSpan KeepAliveTimeout = TimeSpan.FromMinutes(2);

    /// <summary>
    /// Kestrel's <c>MinRequestBodyDataRate</c> is left at its default, which is what drops a client that sends a body
    /// too slowly. Its response rate check only runs while the server has bytes queued, so a quiet stream passes it.
    /// </summary>
    public static void AddRequestLimits(this WebApplicationBuilder builder)
    {
        builder.WebHost.ConfigureKestrel(kestrel =>
        {
            kestrel.Limits.MaxRequestBodySize = MaxBodyBytes;
            kestrel.Limits.RequestHeadersTimeout = HeadersTimeout;
            kestrel.Limits.KeepAliveTimeout = KeepAliveTimeout;
        });
        builder.Services.AddRequestTimeouts(options => options.DefaultPolicy = TimeoutPolicy(RequestTimeout));
    }

    /// <summary>The default policy, with the timeout as a parameter so a test can use a short one.</summary>
    public static RequestTimeoutPolicy TimeoutPolicy(TimeSpan timeout) => new()
    {
        Timeout = timeout,
        TimeoutStatusCode = StatusCodes.Status504GatewayTimeout,
        WriteTimeoutResponse = context => ApiError.Write(
            context, StatusCodes.Status504GatewayTimeout, "request_timeout", "The request took too long."),
    };
}
