using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk.Tests.Support;

internal static class Harness
{
    public const string Key = "sdk-key-sentinel";

    /// <summary>A client over a loopback server with short intervals, no stream by default, on the real clock.</summary>
    public static ToggleSlashClient Client(LoopbackServer server, Action<ClientOptions>? configure = null)
    {
        var options = new ClientOptions
        {
            Url = server.Url,
            SdkKey = Key,
            Stream = false,
            PollInterval = TimeSpan.FromMilliseconds(50),
            FlushInterval = TimeSpan.FromMilliseconds(50),
            RequestTimeout = TimeSpan.FromSeconds(5),
        };
        configure?.Invoke(options);
        return ToggleSlashClient.Create(options);
    }

    public static JsonValue Json(string text) => JsonValue.Parse(text) ?? JsonValue.Null;
}
