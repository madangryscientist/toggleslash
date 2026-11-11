using System.Diagnostics;
using System.Net;
using System.Net.Sockets;
using System.Text;

namespace ToggleSlash.Sdk.Tests;

/// <summary>
/// Contract 7.2, "Bounds": a stream that gets no bytes is dropped as <c>timeout</c>, and a keep-alive restarts the
/// clock. These tests read a real loopback socket that goes quiet, because a test through a handler can pass while
/// the timeout never reaches the socket. The watchdog's length is the <c>StreamIdleTimeout</c> seam; nothing waits 60 s.
/// </summary>
public sealed class WatchdogTests
{
    private static readonly TimeSpan Idle = TimeSpan.FromMilliseconds(600);

    [Fact]
    public async Task A_stream_that_goes_quiet_is_dropped_as_a_timeout_after_the_idle_limit()
    {
        await using var server = new Loopback(async (socket, quit) =>
        {
            await socket.Event("connected", "\"v1\"");
            await Task.Delay(Timeout.Infinite, quit);
        });
        var errors = new Errors();
        var failedAt = new TaskCompletionSource<long>(TaskCreationOptions.RunContinuationsAsynchronously);
        var watch = Stopwatch.StartNew();
        using var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = server.Url,
            SdkKey = "key",
            StreamIdleTimeout = Idle,
            OnError = e =>
            {
                errors.Add(e);
                if (e is TimeoutException) failedAt.TrySetResult(watch.ElapsedMilliseconds);
            },
        });
        Assert.True(await client.ReadyAsync());
        await Wait.Until(() => client.Status.Stream == StreamState.Connected, "the stream to connect");
        var connectedAt = watch.ElapsedMilliseconds;
        var timedOutAt = await failedAt.Task.WaitAsync(TimeSpan.FromSeconds(10));
        Assert.True(timedOutAt - connectedAt >= Idle.TotalMilliseconds * 0.8, $"dropped after {timedOutAt - connectedAt} ms");
        Assert.Contains(errors.All, e => e is TimeoutException);
    }

    [Fact]
    public async Task A_keep_alive_restarts_the_idle_limit()
    {
        // A longer limit than the class constant, so a scheduling stall on a loaded runner cannot trip it.
        var idle = TimeSpan.FromSeconds(3);
        long stoppedAt = 0;
        var watch = Stopwatch.StartNew();
        await using var server = new Loopback(async (socket, quit) =>
        {
            await socket.Event("connected", "\"v1\"");
            // Comments for two idle limits, each a sixth of the limit apart: none changes the ruleset.
            for (var i = 0; i < 12; i++)
            {
                await Task.Delay(idle / 6, quit);
                await socket.Comment("keep-alive");
            }
            stoppedAt = watch.ElapsedMilliseconds;
            await Task.Delay(Timeout.Infinite, quit);
        });
        var firstTimeout = new TaskCompletionSource<long>(TaskCreationOptions.RunContinuationsAsynchronously);
        using var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = server.Url,
            SdkKey = "key",
            StreamIdleTimeout = idle,
            OnError = e =>
            {
                if (e is TimeoutException) firstTimeout.TrySetResult(watch.ElapsedMilliseconds);
            },
        });
        Assert.True(await client.ReadyAsync());
        var timedOutAt = await firstTimeout.Task.WaitAsync(TimeSpan.FromSeconds(30));
        Assert.True(Volatile.Read(ref stoppedAt) > 0, "the idle limit fired while keep-alives were still arriving");
        Assert.True(timedOutAt - Volatile.Read(ref stoppedAt) >= idle.TotalMilliseconds * 0.5, $"{timedOutAt - stoppedAt} ms after the last keep-alive");
    }

    [Fact]
    public async Task A_stream_that_never_answers_its_request_is_dropped_by_the_request_timeout()
    {
        await using var server = new Loopback(async (socket, quit) =>
        {
            // Accepts the stream request and never sends a header.
            await Task.Delay(Timeout.Infinite, quit);
            await socket.Comment("unreachable");
        });
        var timeout = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        using var client = ToggleSlashClient.Create(new ClientOptions
        {
            Url = server.Url,
            SdkKey = "key",
            RequestTimeout = TimeSpan.FromMilliseconds(500),
            StreamIdleTimeout = TimeSpan.FromHours(1),
            OnError = e =>
            {
                if (e is TimeoutException || e is OperationCanceledException) timeout.TrySetResult();
            },
        });
        Assert.True(await client.ReadyAsync());
        await Wait.Until(() => client.Status.LastError == "timeout", "the stream open to time out");
        Assert.Equal(StreamState.Disconnected, client.Status.Stream);
    }

    /// <summary>A loopback HTTP server: the ruleset route answers at once; the stream route runs a script.</summary>
    private sealed class Loopback : IAsyncDisposable
    {
        private readonly TcpListener _listener = new(IPAddress.Loopback, 0);
        private readonly CancellationTokenSource _quit = new();
        private readonly Func<Pipe, CancellationToken, Task> _script;
        private readonly Task _accepting;

        public Loopback(Func<Pipe, CancellationToken, Task> script)
        {
            _script = script;
            _listener.Start();
            _accepting = Task.Run(Accept);
        }

        public string Url => $"http://127.0.0.1:{((IPEndPoint)_listener.LocalEndpoint).Port}";

        private async Task Accept()
        {
            try
            {
                for (; ; )
                {
                    var tcp = await _listener.AcceptTcpClientAsync(_quit.Token);
                    _ = Task.Run(() => Serve(tcp));
                }
            }
            catch (OperationCanceledException)
            {
                // Disposed.
            }
        }

        private async Task Serve(TcpClient tcp)
        {
            using (tcp)
            {
                try
                {
                    var stream = tcp.GetStream();
                    var head = await ReadHead(stream);
                    if (head.Contains("/sdk/v1/stream", StringComparison.Ordinal))
                    {
                        var pipe = new Pipe(stream);
                        await _script(pipe, _quit.Token);
                    }
                    else
                    {
                        const string Body = "{\"flags\":[],\"segments\":[]}";
                        var reply = $"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nETag: \"v1\"\r\nContent-Length: {Body.Length}\r\nConnection: close\r\n\r\n{Body}";
                        await stream.WriteAsync(Encoding.ASCII.GetBytes(reply), _quit.Token);
                    }
                }
                catch (Exception error) when (error is IOException or OperationCanceledException or SocketException or ObjectDisposedException)
                {
                    // The client went away, which is what these tests make it do.
                }
            }
        }

        private static async Task<string> ReadHead(NetworkStream stream)
        {
            var text = new StringBuilder();
            var one = new byte[1];
            while (!text.ToString().EndsWith("\r\n\r\n", StringComparison.Ordinal))
            {
                if (await stream.ReadAsync(one) == 0) break;
                text.Append((char)one[0]);
            }
            return text.ToString();
        }

        public async ValueTask DisposeAsync()
        {
            await _quit.CancelAsync();
            _listener.Stop();
            await _accepting;
            _quit.Dispose();
        }
    }

    /// <summary>A chunked <c>text/event-stream</c> response, headers sent on first use.</summary>
    private sealed class Pipe(NetworkStream stream)
    {
        private bool _started;

        private async Task Write(string text)
        {
            if (!_started)
            {
                _started = true;
                var head = "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\nCache-Control: no-cache\r\n\r\n";
                await stream.WriteAsync(Encoding.ASCII.GetBytes(head));
            }
            var bytes = Encoding.UTF8.GetBytes(text);
            await stream.WriteAsync(Encoding.ASCII.GetBytes($"{bytes.Length:x}\r\n"));
            await stream.WriteAsync(bytes);
            await stream.WriteAsync("\r\n"u8.ToArray());
            await stream.FlushAsync();
        }

        public Task Event(string name, string data) => Write($"event: {name}\ndata: {data}\n\n");

        public Task Comment(string text) => Write($": {text}\n\n");
    }
}
