using System.Text;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Data;
using ToggleSlash.Server.Limits;
using Microsoft.EntityFrameworkCore;

namespace ToggleSlash.Server.Sdk;

/// <summary>When a stream's next keep-alive is due. A seam so a test can tick it instead of waiting.</summary>
public class StreamHeartbeat
{
    public static readonly TimeSpan Interval = TimeSpan.FromSeconds(25);

    public virtual Task Next(CancellationToken cancellation) => Task.Delay(Interval, cancellation);
}

/// <summary>
/// <c>GET /sdk/v1/stream</c>: Server-Sent Events saying what the environment's ruleset's ETag is, on connect
/// and on every change. Written by hand because a keep-alive is a comment, which <c>TypedResults.ServerSentEvents</c>
/// cannot send. A stream outlives any one read, so it holds no database context: each read gets its own scope.
/// </summary>
internal static class RulesetStream
{
    private const string Connected = "connected";
    private const string Changed = "changed";

    public static async Task Run(
        HttpContext http,
        RulesetNotifier notifier,
        StreamHeartbeat heartbeat,
        IServiceScopeFactory scopes,
        IHostApplicationLifetime lifetime,
        RequestLimiter limiter)
    {
        var environmentId = ServerAuth.EnvironmentId(http.User);
        var keyId = ServerAuth.KeyId(http.User);
        // A using, so the slot comes back on every way out: the client leaving, an error, the host stopping.
        using var slot = limiter.TryOpenStream(keyId);
        if (slot is null)
        {
            await RateLimiting.Write(http, RequestLimiter.StreamRetryAfterSeconds, "This key has too many streams open.");
            return;
        }
        // The host waits for open requests when it stops, so a stream has to end on the stopping signal too.
        using var ending = CancellationTokenSource.CreateLinkedTokenSource(http.RequestAborted, lifetime.ApplicationStopping);
        var cancellation = ending.Token;
        // Before the first read: a write between the two then wakes the loop, not nothing.
        using var subscription = notifier.Subscribe(environmentId);

        try
        {
            http.Response.ContentType = "text/event-stream";
            http.Response.Headers.CacheControl = "no-cache";

            var sent = await CurrentETag(scopes, environmentId, cancellation);
            await Write(http, Event(Connected, sent), cancellation);

            var change = subscription.Next(cancellation);
            var beat = heartbeat.Next(cancellation);
            while (true)
            {
                var woken = await Task.WhenAny(change, beat);
                await woken;
                if (!await KeyIsLive(scopes, keyId, cancellation))
                {
                    return;
                }

                if (woken == beat)
                {
                    beat = heartbeat.Next(cancellation);
                    await Write(http, ": keep-alive\n\n", cancellation);
                    continue;
                }

                change = subscription.Next(cancellation);
                var current = await CurrentETag(scopes, environmentId, cancellation);
                // A write can leave the served bytes as they were (archiving an archived flag). The client has them.
                if (current != sent)
                {
                    sent = current;
                    await Write(http, Event(Changed, current), cancellation);
                }
            }
        }
        catch (OperationCanceledException) when (cancellation.IsCancellationRequested)
        {
            // The client left or the host is stopping: either way this is how a stream ends.
        }
    }

    /// <summary>The data is the tag exactly as the ruleset route's <c>ETag</c> header gives it, quotes included.</summary>
    private static string Event(string name, string etag) => $"event: {name}\ndata: {etag}\n\n";

    private static async Task Write(HttpContext http, string text, CancellationToken cancellation)
    {
        await http.Response.Body.WriteAsync(Encoding.UTF8.GetBytes(text), cancellation);
        await http.Response.Body.FlushAsync(cancellation);
    }

    private static async Task<string> CurrentETag(IServiceScopeFactory scopes, long environmentId, CancellationToken cancellation)
    {
        await using var scope = scopes.CreateAsyncScope();
        var snapshot = await scope.ServiceProvider.GetRequiredService<RulesetReader>().Read(environmentId, cancellation);
        return snapshot.ETag;
    }

    private static async Task<bool> KeyIsLive(IServiceScopeFactory scopes, long keyId, CancellationToken cancellation)
    {
        await using var scope = scopes.CreateAsyncScope();
        return await scope.ServiceProvider.GetRequiredService<ToggleSlashDbContext>().ApiKeys
            .AnyAsync(k => k.Id == keyId && k.RevokedAt == null, cancellation);
    }
}
