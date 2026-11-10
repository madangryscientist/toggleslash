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

    /// <summary>
    /// Moves a fake clock forward in steps. Before the first step and after every one it waits, on real time, until
    /// the SDK has armed at least <paramref name="timers"/> timers again (its poll or hold loop and its flush loop),
    /// because a step taken while a loop is between waking and re-arming is a step that loop never sees: the clock
    /// would run on without it and the test would depend on how fast the machine happens to be.
    /// </summary>
    public static async Task Advance(Clock time, TimeSpan total, TimeSpan? step = null, int timers = 1)
    {
        var each = step ?? TimeSpan.FromSeconds(1);
        await Armed(time, timers);
        for (var done = TimeSpan.Zero; done < total; done += each)
        {
            time.Advance(each < total - done ? each : total - done);
            await Task.Delay(2);
            await Armed(time, timers);
        }
    }

    private static Task Armed(Clock time, int timers) => Until(() => time.Armed >= timers, $"{timers} timer(s) to be armed", TimeSpan.FromSeconds(30));
}

/// <summary>
/// A <see cref="Microsoft.Extensions.Time.Testing.FakeTimeProvider"/> that knows how many of its timers are armed
/// (due, not yet fired, not disposed), so a test can tell when the SDK's loops are waiting on the clock.
/// </summary>
internal sealed class Clock(DateTimeOffset start) : Microsoft.Extensions.Time.Testing.FakeTimeProvider(start)
{
    private int _armed;

    public int Armed => Volatile.Read(ref _armed);

    public override ITimer CreateTimer(TimerCallback callback, object? state, TimeSpan dueTime, TimeSpan period)
    {
        Counted? counted = null;
        var inner = base.CreateTimer(
            s =>
            {
                // A one-shot timer is spent the moment it fires; a periodic one stays armed.
                if (period == Timeout.InfiniteTimeSpan || period == TimeSpan.Zero) counted!.Disarm();
                callback(s);
            },
            state,
            dueTime,
            period);
        counted = new Counted(this, inner, dueTime != Timeout.InfiniteTimeSpan);
        return counted;
    }

    private sealed class Counted : ITimer
    {
        private readonly Clock _owner;
        private readonly ITimer _inner;
        private int _on;

        public Counted(Clock owner, ITimer inner, bool armed)
        {
            _owner = owner;
            _inner = inner;
            if (armed) Arm();
        }

        private void Arm()
        {
            if (Interlocked.Exchange(ref _on, 1) == 0) Interlocked.Increment(ref _owner._armed);
        }

        public void Disarm()
        {
            if (Interlocked.Exchange(ref _on, 0) == 1) Interlocked.Decrement(ref _owner._armed);
        }

        public bool Change(TimeSpan dueTime, TimeSpan period)
        {
            if (dueTime == Timeout.InfiniteTimeSpan) Disarm();
            else Arm();
            return _inner.Change(dueTime, period);
        }

        public void Dispose()
        {
            Disarm();
            _inner.Dispose();
        }

        public ValueTask DisposeAsync()
        {
            Disarm();
            return _inner.DisposeAsync();
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
