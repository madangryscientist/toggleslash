using System.Collections.Concurrent;
using System.Net;
using System.Net.Http.Headers;
using System.Text;

namespace ToggleSlash.Sdk.Tests;

/// <summary>One request the SDK made, as the fake server saw it.</summary>
internal sealed record Seen(string Method, string Path, IReadOnlyDictionary<string, string> Headers, string Body, DateTimeOffset At);

/// <summary>An <see cref="HttpMessageHandler"/> that records every request and answers from a function. No socket.</summary>
internal sealed class FakeServer(TimeProvider time, Func<Seen, CancellationToken, Task<HttpResponseMessage>> answer) : HttpMessageHandler
{
    private readonly ConcurrentQueue<Seen> _seen = new();

    public IReadOnlyCollection<Seen> Requests => _seen;

    public List<Seen> To(string path) => [.. _seen.Where(s => s.Path == path)];

    protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
    {
        var body = request.Content is null ? "" : await request.Content.ReadAsStringAsync(cancellationToken);
        var headers = request.Headers
            .Concat(request.Content?.Headers ?? Enumerable.Empty<KeyValuePair<string, IEnumerable<string>>>())
            .ToDictionary(h => h.Key, h => string.Join(",", h.Value), StringComparer.OrdinalIgnoreCase);
        var seen = new Seen(request.Method.Method, request.RequestUri!.AbsolutePath, headers, body, time.GetUtcNow());
        _seen.Enqueue(seen);
        return await answer(seen, cancellationToken);
    }
}

/// <summary>Canned answers.</summary>
internal static class Reply
{
    public const string Ruleset = "/sdk/v1/ruleset";
    public const string Stream = "/sdk/v1/stream";
    public const string Events = "/sdk/v1/events";

    public static Task<HttpResponseMessage> Rules(string flagsJson, string etag)
    {
        var response = new HttpResponseMessage(HttpStatusCode.OK)
        {
            Content = new StringContent($"{{\"flags\":[{flagsJson}],\"segments\":[]}}", Encoding.UTF8, "application/json"),
        };
        response.Headers.TryAddWithoutValidation("ETag", etag);
        return Task.FromResult(response);
    }

    public static Task<HttpResponseMessage> Status(int code, string? retryAfter = null)
    {
        var response = new HttpResponseMessage((HttpStatusCode)code) { Content = new StringContent("") };
        if (retryAfter is not null) response.Headers.TryAddWithoutValidation("Retry-After", retryAfter);
        return Task.FromResult(response);
    }

    /// <summary>Never answers, until the request is cancelled.</summary>
    public static async Task<HttpResponseMessage> Hang(CancellationToken cancellation)
    {
        await Task.Delay(Timeout.InfiniteTimeSpan, cancellation);
        throw new InvalidOperationException("unreachable");
    }
}

internal static class Flags
{
    /// <summary>A flag that is on and serves <paramref name="valueJson"/> to everyone.</summary>
    public static string Constant(string key, string valueJson, int version = 1) =>
        $"{{\"key\":\"{key}\",\"variations\":[{valueJson}],\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],\"fallthrough\":{{\"variation\":0}},\"salt\":\"s\",\"version\":{version}}}";
}

internal static class Wait
{
    /// <summary>Polls on real time until <paramref name="condition"/> holds, or fails the test after <paramref name="limit"/>.</summary>
    public static async Task Until(Func<bool> condition, string what, TimeSpan? limit = null)
    {
        var stop = DateTime.UtcNow + (limit ?? TimeSpan.FromSeconds(10));
        while (!condition())
        {
            if (DateTime.UtcNow > stop) Assert.Fail("timed out waiting for " + what);
            await Task.Delay(5);
        }
    }

    /// <summary>Moves a fake clock forward in steps, giving the SDK's tasks real time to run between steps.</summary>
    public static async Task Advance(Microsoft.Extensions.Time.Testing.FakeTimeProvider time, TimeSpan total, TimeSpan? step = null)
    {
        var each = step ?? TimeSpan.FromSeconds(1);
        for (var done = TimeSpan.Zero; done < total; done += each)
        {
            time.Advance(each < total - done ? each : total - done);
            await Task.Delay(8);
        }
    }
}

internal sealed class Errors
{
    private readonly ConcurrentQueue<Exception> _errors = new();

    public void Add(Exception error) => _errors.Enqueue(error);

    public List<Exception> All => [.. _errors];

    public int Count => _errors.Count;
}
