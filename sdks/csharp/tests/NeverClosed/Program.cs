using ToggleSlash.Sdk;

// Creates a client that is never closed, uses it, and returns from Main. The test that runs this program checks it
// exits by itself: the client's background work must not keep the process alive (contract 6.6).
var client = ToggleSlashClient.Create(new ClientOptions
{
    Url = "http://127.0.0.1:9",
    SdkKey = "key",
    OnError = _ => { },
});
Console.Out.WriteLine(client.BoolVariation("f", Context.ForKey("u1"), true));
return 0;
