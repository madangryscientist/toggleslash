using System.Collections.Concurrent;
using System.Globalization;
using System.Text.Json;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting.Server;
using Microsoft.AspNetCore.Hosting.Server.Features;
using Microsoft.AspNetCore.Http;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;
using ToggleSlash.Sdk;

// The test service of docs/sdk-test-service.md: the SDK behind a small HTTP server, driven by the conformance
// harness. It listens on 127.0.0.1 and a port the OS picks, and prints `listening <url>` once it does.
var builder = WebApplication.CreateSlimBuilder(args);
builder.Logging.ClearProviders();
builder.WebHost.UseUrls("http://127.0.0.1:0");
var app = builder.Build();

var clients = new ConcurrentDictionary<string, Entry>(StringComparer.Ordinal);
var created = 0;

app.Run(async http =>
{
    try
    {
        await Route(http);
    }
    catch (Exception error)
    {
        // The SDK must not throw; when anything does, the harness is told, not spared.
        http.Response.StatusCode = StatusCodes.Status500InternalServerError;
        http.Response.ContentType = "text/plain";
        await http.Response.WriteAsync(error.ToString());
    }
});

await app.StartAsync();
var address = app.Services.GetRequiredService<IServer>().Features.Get<IServerAddressesFeature>()!.Addresses.First();
await Console.Out.WriteLineAsync($"listening {address}");
await Console.Out.FlushAsync();
await app.WaitForShutdownAsync();
return 0;

async Task Route(HttpContext http)
{
    var method = http.Request.Method;
    var path = http.Request.Path.Value ?? "/";
    if (method == "GET" && path == "/")
    {
        await Reply(http, w =>
        {
            w.WriteStartObject();
            w.WriteString("name", "csharp");
            w.WriteStartArray("capabilities");
            w.WriteStringValue("integer-accessor");
            w.WriteEndArray();
            w.WriteEndObject();
        });
        return;
    }
    if (method == "POST" && path == "/clients")
    {
        using var body = await JsonDocument.ParseAsync(http.Request.Body);
        var id = Interlocked.Increment(ref created).ToString(CultureInfo.InvariantCulture);
        var entry = new Entry();
        entry.Client = ToggleSlashClient.Create(OptionsFrom(body.RootElement, entry));
        clients[id] = entry;
        http.Response.StatusCode = StatusCodes.Status201Created;
        await Reply(http, w =>
        {
            w.WriteStartObject();
            w.WriteString("id", id);
            w.WriteEndObject();
        });
        return;
    }
    var parts = path.Split('/', StringSplitOptions.RemoveEmptyEntries);
    if (parts is ["clients", var clientId, var action] && clients.TryGetValue(clientId, out var found))
    {
        await ClientRoute(http, found, $"{method} {action}");
        return;
    }
    http.Response.StatusCode = StatusCodes.Status404NotFound;
}

async Task ClientRoute(HttpContext http, Entry entry, string route)
{
    var client = entry.Client!;
    switch (route)
    {
        case "POST ready":
            var ready = await client.ReadyAsync();
            await Reply(http, w =>
            {
                w.WriteStartObject();
                w.WriteBoolean("ready", ready);
                w.WriteEndObject();
            });
            break;
        case "POST evaluate":
            using (var body = await JsonDocument.ParseAsync(http.Request.Body))
            {
                var answer = Evaluate(client, body.RootElement);
                await Reply(http, answer);
            }
            break;
        case "GET status":
            var status = client.Status;
            await Reply(http, w =>
            {
                w.WriteStartObject();
                WriteNullable(w, "etag", status.Etag);
                if (status.LastSyncAt is { } at) w.WriteNumber("lastSyncAt", at.ToUnixTimeMilliseconds());
                else w.WriteNull("lastSyncAt");
                w.WriteString("stream", status.Stream.Name);
                WriteNullable(w, "lastError", status.LastError);
                w.WriteEndObject();
            });
            break;
        case "GET hook-events":
            var events = entry.HookEvents();
            await Reply(http, w =>
            {
                w.WriteStartObject();
                w.WriteStartArray("events");
                foreach (var e in events) WriteEvent(w, e);
                w.WriteEndArray();
                w.WriteEndObject();
            });
            break;
        case "GET errors":
            var messages = entry.Errors();
            await Reply(http, w =>
            {
                w.WriteStartObject();
                w.WriteNumber("count", messages.Count);
                w.WriteStartArray("messages");
                foreach (var m in messages) w.WriteStringValue(m);
                w.WriteEndArray();
                w.WriteEndObject();
            });
            break;
        case "POST close":
            await client.CloseAsync();
            await Reply(http, w =>
            {
                w.WriteStartObject();
                w.WriteEndObject();
            });
            break;
        default:
            http.Response.StatusCode = StatusCodes.Status404NotFound;
            break;
    }
}

static ClientOptions OptionsFrom(JsonElement body, Entry entry)
{
    var onErrorThrows = Flag(body, "onErrorThrows");
    var hookThrows = Flag(body, "hookThrows");
    var defaults = new ClientOptions();
    return new ClientOptions
    {
        Url = body.GetProperty("url").GetString(),
        SdkKey = body.GetProperty("sdkKey").GetString(),
        Stream = body.TryGetProperty("stream", out var stream) && stream.ValueKind is JsonValueKind.True or JsonValueKind.False
            ? stream.GetBoolean()
            : true,
        PollInterval = Millis(body, "pollIntervalMs") ?? defaults.PollInterval,
        FlushInterval = Millis(body, "flushIntervalMs") ?? defaults.FlushInterval,
        RequestTimeout = Millis(body, "requestTimeoutMs") ?? defaults.RequestTimeout,
        OnError = error =>
        {
            entry.AddError(error.Message);
            if (onErrorThrows) throw new InvalidOperationException("onError threw, as the test asked");
        },
        Hooks = Flag(body, "hook") || hookThrows
            ? new Hooks
            {
                AfterEvaluation = e =>
                {
                    entry.AddEvent(e);
                    if (hookThrows) throw new InvalidOperationException("the hook threw, as the test asked");
                },
            }
            : null,
    };
}

static bool Flag(JsonElement body, string name) => body.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.True;

static TimeSpan? Millis(JsonElement body, string name) =>
    body.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.Number ? TimeSpan.FromMilliseconds(v.GetDouble()) : null;

static Action<Utf8JsonWriter> Evaluate(ToggleSlashClient client, JsonElement body)
{
    var flag = body.GetProperty("flagKey").GetString()!;
    var context = Context.FromElement(body.GetProperty("context").Clone());
    var fallback = body.GetProperty("fallback");
    switch (body.GetProperty("accessor").GetString())
    {
        case "bool":
            return Answer(client.BoolVariationDetail(flag, context, fallback.GetBoolean()), (w, v) => w.WriteBooleanValue(v));
        case "string":
            return Answer(client.StringVariationDetail(flag, context, fallback.GetString()!), (w, v) => w.WriteStringValue(v));
        case "number":
            return Answer(client.DoubleVariationDetail(flag, context, fallback.GetDouble()), (w, v) => w.WriteNumberValue(v));
        case "int":
            var whole = fallback.TryGetInt64(out var l) ? l : (long)fallback.GetDouble();
            return Answer(client.IntVariationDetail(flag, context, whole), (w, v) => w.WriteNumberValue(v));
        default:
            return Answer(client.JsonVariationDetail(flag, context, fallback.Clone()), (w, v) => v.WriteTo(w));
    }
}

static Action<Utf8JsonWriter> Answer<T>(VariationDetail<T> detail, Action<Utf8JsonWriter, T> writeValue) =>
    w =>
    {
        w.WriteStartObject();
        w.WritePropertyName("value");
        writeValue(w, detail.Value);
        if (detail.VariationIndex is { } index) w.WriteNumber("variationIndex", index);
        else w.WriteNull("variationIndex");
        w.WritePropertyName("reason");
        WriteReason(w, detail.Reason);
        w.WriteEndObject();
    };

static void WriteReason(Utf8JsonWriter w, Reason reason)
{
    w.WriteStartObject();
    w.WriteString("kind", reason.Kind.Name);
    if (reason.Code is { } code) w.WriteString("code", code.Name);
    if (reason.RuleIndex is { } rule) w.WriteNumber("ruleIndex", rule);
    if (reason.FlagKey is { } flagKey) w.WriteString("flagKey", flagKey);
    w.WriteEndObject();
}

static void WriteEvent(Utf8JsonWriter w, EvaluationEvent e)
{
    w.WriteStartObject();
    w.WriteString("flagKey", e.FlagKey);
    w.WritePropertyName("context");
    e.Context.Json.WriteTo(w);
    w.WritePropertyName("value");
    e.Value.WriteTo(w);
    if (e.VariationIndex is { } index) w.WriteNumber("variationIndex", index);
    else w.WriteNull("variationIndex");
    w.WritePropertyName("reason");
    WriteReason(w, e.Reason);
    WriteNullable(w, "etag", e.Etag);
    if (e.FlagVersion is { } version) w.WriteNumber("flagVersion", version);
    else w.WriteNull("flagVersion");
    w.WriteEndObject();
}

static void WriteNullable(Utf8JsonWriter w, string name, string? value)
{
    if (value is null) w.WriteNull(name);
    else w.WriteString(name, value);
}

static async Task Reply(HttpContext http, Action<Utf8JsonWriter> write)
{
    using var stream = new MemoryStream();
    await using (var writer = new Utf8JsonWriter(stream))
    {
        write(writer);
    }
    http.Response.ContentType = "application/json";
    stream.Position = 0;
    await stream.CopyToAsync(http.Response.Body);
}

/// <summary>One client the service holds, with what its callbacks recorded.</summary>
internal sealed class Entry
{
    private readonly Lock _gate = new();
    private readonly List<EvaluationEvent> _events = [];
    private readonly List<string> _errors = [];

    public ToggleSlashClient? Client { get; set; }

    public void AddEvent(EvaluationEvent e)
    {
        lock (_gate) _events.Add(e);
    }

    public void AddError(string message)
    {
        lock (_gate) _errors.Add(message);
    }

    public List<EvaluationEvent> HookEvents()
    {
        lock (_gate) return [.. _events];
    }

    public List<string> Errors()
    {
        lock (_gate) return [.. _errors];
    }
}
