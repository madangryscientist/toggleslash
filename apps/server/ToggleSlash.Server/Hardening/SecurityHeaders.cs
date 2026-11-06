namespace ToggleSlash.Server.Hardening;

/// <summary>
/// The headers every response carries. Set in <c>OnStarting</c>, so they ride the exception handler's 500, a 401, a
/// 404, a 413, a 429 and the first flush of a stream alike: the handler clears headers a middleware set up front,
/// and a callback runs after that. Nothing here touches CORS, <c>ETag</c> or the route's own <c>Cache-Control</c>.
/// There is no <c>Strict-Transport-Security</c>: the server does not terminate TLS, so that header belongs to
/// whatever does (<c>docs/self-hosting.md</c>).
/// </summary>
public static class SecurityHeaders
{
    private const string AdminPath = "/api";

    public static void UseSecurityHeaders(this WebApplication app) => app.Use((context, next) =>
    {
        context.Response.OnStarting(static state =>
        {
            var context = (HttpContext)state;
            var headers = context.Response.Headers;
            headers.XContentTypeOptions = "nosniff";
            headers["Referrer-Policy"] = "no-referrer";
            headers.XFrameOptions = "DENY";
            // The admin API answers with plaintext SDK keys and every flag's rules, and its callers are not
            // browsers that would cache it on purpose. The ruleset and stream routes keep their own policy.
            if (context.Request.Path.StartsWithSegments(AdminPath))
            {
                headers.CacheControl = "no-store";
            }
            return Task.CompletedTask;
        }, context);
        return next(context);
    });
}
