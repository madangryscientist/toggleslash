using System.Net;
using System.Net.Sockets;
using ToggleSlash.Sdk;

// Creates a client that is mid-request to a server that never answers, and returns from Main without closing it.
// The test runs this program and checks the process still exits.
var listener = new TcpListener(IPAddress.Loopback, 0);
listener.Start();
_ = Task.Run(async () =>
{
    while (true)
    {
        _ = await listener.AcceptTcpClientAsync();
    }
});

var client = ToggleSlashClient.Create(new ClientOptions
{
    Url = $"http://127.0.0.1:{((IPEndPoint)listener.LocalEndpoint).Port}",
    SdkKey = "k",
    RequestTimeout = TimeSpan.FromMinutes(5),
});
await Task.Delay(300);
Console.WriteLine("returning from Main with the client open: " + client.Status().Stream);
