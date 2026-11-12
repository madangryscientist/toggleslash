using System.Net;
using System.Net.Security;
using System.Net.Sockets;
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;
using System.Text;

namespace ToggleSlash.Sdk.Tests.Support;

/// <summary>One request as the server read it: the headers exactly as they arrived, in order.</summary>
internal sealed record Recorded(string Method, string Path, IReadOnlyList<KeyValuePair<string, string>> Headers, string Body)
{
    public string? Header(string name) =>
        Headers.FirstOrDefault(h => string.Equals(h.Key, name, StringComparison.OrdinalIgnoreCase)).Value;
}

/// <summary>What a handler does with one connection. Every connection serves one request and is then closed.</summary>
internal sealed class Connection(Stream stream, TcpClient client)
{
    private static readonly UTF8Encoding Utf8 = new(false);

    /// <summary>An answer with a body and a Content-Length.</summary>
    public async Task SendAsync(int status, string body = "", params (string Name, string Value)[] headers)
    {
        var bytes = Utf8.GetBytes(body);
        var head = new StringBuilder($"HTTP/1.1 {status} X\r\n");
        foreach (var (name, value) in headers)
        {
            head.Append(name).Append(": ").Append(value).Append("\r\n");
        }

        head.Append("Content-Length: ").Append(bytes.Length).Append("\r\nConnection: close\r\n\r\n");
        await stream.WriteAsync(Utf8.GetBytes(head.ToString()));
        await stream.WriteAsync(bytes);
        await stream.FlushAsync();
    }

    /// <summary>The head of a body that follows over time (no length: it ends when the connection does).</summary>
    public async Task HeadAsync(int status, params (string Name, string Value)[] headers)
    {
        var head = new StringBuilder($"HTTP/1.1 {status} X\r\n");
        foreach (var (name, value) in headers)
        {
            head.Append(name).Append(": ").Append(value).Append("\r\n");
        }

        head.Append("Connection: close\r\n\r\n");
        await stream.WriteAsync(Utf8.GetBytes(head.ToString()));
        await stream.FlushAsync();
    }

    public async Task WriteAsync(string text)
    {
        await stream.WriteAsync(Utf8.GetBytes(text));
        await stream.FlushAsync();
    }

    /// <summary>Answers nothing until the server is disposed or the client goes.</summary>
    public async Task HangAsync(CancellationToken stop)
    {
        try
        {
            await Task.Delay(Timeout.Infinite, stop);
        }
        catch (OperationCanceledException)
        {
            // The server is being disposed.
        }
    }

    /// <summary>Closes the socket with a reset, not a clean end.</summary>
    public void Reset()
    {
        client.LingerState = new LingerOption(true, 0);
        client.Close();
    }
}

/// <summary>
/// A hand-written loopback HTTP/1.1 server that records each request's headers byte for byte, so a test sees what the
/// client library added by itself. With <c>tls</c> it speaks TLS with a throwaway self-signed certificate.
/// </summary>
internal sealed class LoopbackServer : IDisposable
{
    private readonly TcpListener _listener = new(IPAddress.Loopback, 0);
    private readonly CancellationTokenSource _stop = new();
    private readonly List<Recorded> _requests = [];
    private readonly List<TcpClient> _clients = [];
    private readonly Func<Recorded, Connection, CancellationToken, Task> _handler;
    private readonly X509Certificate2? _certificate;

    public LoopbackServer(Func<Recorded, Connection, CancellationToken, Task> handler, bool tls = false)
    {
        _handler = handler;
        if (tls)
        {
            using var key = RSA.Create(2048);
            var request = new CertificateRequest("CN=localhost", key, HashAlgorithmName.SHA256, RSASignaturePadding.Pkcs1);
            using var ephemeral = request.CreateSelfSigned(DateTimeOffset.UtcNow.AddDays(-1), DateTimeOffset.UtcNow.AddDays(1));
            _certificate = X509CertificateLoader.LoadPkcs12(ephemeral.Export(X509ContentType.Pfx), null);
        }

        _listener.Start();
        Url = $"{(tls ? "https" : "http")}://127.0.0.1:{((IPEndPoint)_listener.LocalEndpoint).Port}";
        _ = Task.Run(AcceptLoopAsync);
    }

    public string Url { get; }

    public IReadOnlyList<Recorded> Requests
    {
        get
        {
            lock (_requests)
            {
                return [.. _requests];
            }
        }
    }

    public IReadOnlyList<Recorded> To(string path) => [.. Requests.Where(r => r.Path == path)];

    private async Task AcceptLoopAsync()
    {
        try
        {
            while (!_stop.IsCancellationRequested)
            {
                var client = await _listener.AcceptTcpClientAsync(_stop.Token);
                lock (_clients)
                {
                    _clients.Add(client);
                }

                _ = Task.Run(() => ServeAsync(client));
            }
        }
        catch (Exception)
        {
            // The listener was disposed.
        }
    }

    private async Task ServeAsync(TcpClient client)
    {
        try
        {
            Stream stream = client.GetStream();
            if (_certificate is not null)
            {
                var ssl = new SslStream(stream);
                await ssl.AuthenticateAsServerAsync(_certificate);
                stream = ssl;
            }

            var request = await ReadRequestAsync(stream);
            if (request is null)
            {
                return;
            }

            lock (_requests)
            {
                _requests.Add(request);
            }

            await _handler(request, new Connection(stream, client), _stop.Token);
        }
        catch (Exception)
        {
            // A client that went away.
        }
        finally
        {
            client.Close();
        }
    }

    private static async Task<Recorded?> ReadRequestAsync(Stream stream)
    {
        var buffer = new List<byte>();
        var one = new byte[1];
        while (true)
        {
            if (await stream.ReadAsync(one) == 0)
            {
                return null;
            }

            buffer.Add(one[0]);
            var n = buffer.Count;
            if (n >= 4 && buffer[n - 4] == '\r' && buffer[n - 3] == '\n' && buffer[n - 2] == '\r' && buffer[n - 1] == '\n')
            {
                break;
            }
        }

        var lines = Encoding.Latin1.GetString([.. buffer]).Split("\r\n", StringSplitOptions.RemoveEmptyEntries);
        var first = lines[0].Split(' ');
        var headers = new List<KeyValuePair<string, string>>();
        foreach (var line in lines.Skip(1))
        {
            var colon = line.IndexOf(':', StringComparison.Ordinal);
            headers.Add(new(line[..colon], line[(colon + 1)..].Trim()));
        }

        var length = headers.FirstOrDefault(h => string.Equals(h.Key, "content-length", StringComparison.OrdinalIgnoreCase)).Value;
        var body = string.Empty;
        if (length is not null)
        {
            var bytes = new byte[int.Parse(length, System.Globalization.CultureInfo.InvariantCulture)];
            var read = 0;
            while (read < bytes.Length)
            {
                var got = await stream.ReadAsync(bytes.AsMemory(read));
                if (got == 0)
                {
                    break;
                }

                read += got;
            }

            body = Encoding.UTF8.GetString(bytes, 0, read);
        }

        return new Recorded(first[0], first[1], headers, body);
    }

    public void Dispose()
    {
        _stop.Cancel();
        _listener.Stop();
        lock (_clients)
        {
            foreach (var client in _clients)
            {
                client.Close();
            }
        }

        _certificate?.Dispose();
    }
}
