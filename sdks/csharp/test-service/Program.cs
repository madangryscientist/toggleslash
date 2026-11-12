// The C# SDK's conformance test service: an HTTP server that wraps ToggleSlashClient so the harness in
// packages/sdk-conformance can drive it from outside. The protocol is docs/sdk-test-service.md and the accessor
// mapping is in README.md. It catches nothing the SDK throws except to answer 500 with the error text.
using System.Collections.Concurrent;
using System.Globalization;
using System.Text;
using Microsoft.AspNetCore.Hosting.Server;
using Microsoft.AspNetCore.Hosting.Server.Features;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using ToggleSlash.Sdk;
using ToggleSlash.Sdk.Json;

var builder = WebApplication.CreateSlimBuilder(args);
builder.Logging.ClearProviders();
builder.WebHost.UseUrls("http://127.0.0.1:0");
builder.WebHost.ConfigureKestrel(options =>
{
    options.Limits.KeepAliveTimeout = TimeSpan.FromHours(1);
    options.Limits.RequestHeadersTimeout = TimeSpan.FromMinutes(5);
    options.Limits.MaxRequestBodySize = null;
});

var app = builder.Build();
var clients = new ConcurrentDictionary<string, Entry>();
var created = 0;

app.Run(async context =>
{
    try
    {
        await Route(context);
    }
    catch (Exception error)
    {
        context.Response.StatusCode = 500;
        context.Response.ContentType = "text/plain; charset=utf-8";
        await context.Response.WriteAsync(error.ToString());
    }
});

await app.StartAsync();
var address = app.Services.GetRequiredService<IServer>().Features.Get<IServerAddressesFeature>()!.Addresses.First();
var port = new Uri(address).Port;
Console.Out.WriteLine(string.Create(CultureInfo.InvariantCulture, $"listening http://127.0.0.1:{port}"));
Console.Out.Flush();
await app.WaitForShutdownAsync();
return 0;

async Task Route(HttpContext context)
{
    var path = context.Request.Path.Value ?? "/";
    var method = context.Request.Method;
    if (method == "GET" && path == "/")
    {
        await Reply(context, Obj(
            ("name", JsonValue.From("toggleslash-sdk (C#)")),
            ("capabilities", new JsonArray([JsonValue.From("integer-accessor")]))), 200);
        return;
    }

    if (method == "POST" && path == "/clients")
    {
        await Create(context);
        return;
    }

    var parts = path.Split('/', StringSplitOptions.RemoveEmptyEntries);
    if (parts.Length != 3 || parts[0] != "clients" || !clients.TryGetValue(parts[1], out var entry))
    {
        context.Response.StatusCode = 404;
        await context.Response.WriteAsync("no such client");
        return;
    }

    switch ($"{method} {parts[2]}")
    {
        case "POST ready":
            await Reply(context, Obj(("ready", JsonValue.From(await entry.Client.ReadyAsync()))), 200);
            break;
        case "POST evaluate":
            await Evaluate(context, entry);
            break;
        case "GET status":
            await Reply(context, StatusJson(entry.Client.Status()), 200);
            break;
        case "GET hook-events":
            lock (entry.Lock)
            {
                var events = new JsonArray(entry.HookEvents.Select(EventJson));
                Reply(context, Obj(("events", events)), 200).GetAwaiter().GetResult();
            }

            break;
        case "GET errors":
            lock (entry.Lock)
            {
                var messages = new JsonArray(entry.Errors.Select(m => JsonValue.From(m)));
                Reply(context, Obj(("count", JsonValue.From(entry.Errors.Count)), ("messages", messages)), 200).GetAwaiter().GetResult();
            }

            break;
        case "POST close":
            await entry.Client.CloseAsync();
            await Reply(context, Obj(), 200);
            break;
        default:
            context.Response.StatusCode = 404;
            await context.Response.WriteAsync("no such route");
            break;
    }
}

async Task Create(HttpContext context)
{
    var body = await ReadBody(context);
    var entry = new Entry();
    var options = new ClientOptions
    {
        Url = Text(body, "url"),
        SdkKey = Text(body, "sdkKey"),
        OnError = error =>
        {
            lock (entry.Lock)
            {
                entry.Errors.Add(error.Message);
            }

            if (Flag(body, "onErrorThrows"))
            {
                throw new InvalidOperationException("onError threw, as the test asked");
            }
        },
    };
    if (body.TryGetValue("stream", out var stream) && stream is JsonBool streamBool)
    {
        options.Stream = streamBool.Value;
    }

    options.PollInterval = Millis(body, "pollIntervalMs");
    options.FlushInterval = Millis(body, "flushIntervalMs");
    options.RequestTimeout = Millis(body, "requestTimeoutMs");
    if (Flag(body, "hook") || Flag(body, "hookThrows"))
    {
        var throws = Flag(body, "hookThrows");
        options.AfterEvaluation = evaluation =>
        {
            lock (entry.Lock)
            {
                entry.HookEvents.Add(evaluation);
            }

            if (throws)
            {
                throw new InvalidOperationException("the hook threw, as the test asked");
            }
        };
    }

    entry.Client = ToggleSlashClient.Create(options);
    var id = Interlocked.Increment(ref created).ToString(CultureInfo.InvariantCulture);
    clients[id] = entry;
    await Reply(context, Obj(("id", JsonValue.From(id))), 201);
}

async Task Evaluate(HttpContext context, Entry entry)
{
    var body = await ReadBody(context);
    var flagKey = Text(body, "flagKey") ?? string.Empty;
    var evaluationContext = body.TryGetValue("context", out var c) ? c : JsonValue.Null;
    var fallback = body.TryGetValue("fallback", out var f) ? f : JsonValue.Null;
    var accessor = Text(body, "accessor") ?? "any";
    var client = entry.Client;
    JsonValue value;
    int? index;
    Reason reason;
    switch (accessor)
    {
        case "bool":
            {
                var detail = client.BoolVariationDetail(flagKey, evaluationContext, fallback is JsonBool b && b.Value);
                (value, index, reason) = (JsonValue.From(detail.Value), detail.VariationIndex, detail.Reason);
                break;
            }

        case "string":
            {
                var detail = client.StringVariationDetail(flagKey, evaluationContext, (fallback as JsonString)?.Value);
                (value, index, reason) = (JsonValue.From(detail.Value), detail.VariationIndex, detail.Reason);
                break;
            }

        case "number":
            {
                var detail = client.FloatVariationDetail(flagKey, evaluationContext, (fallback as JsonNumber)?.Value ?? 0);
                (value, index, reason) = (JsonValue.From(detail.Value), detail.VariationIndex, detail.Reason);
                break;
            }

        case "int":
            {
                var detail = client.IntVariationDetail(flagKey, evaluationContext, (long)((fallback as JsonNumber)?.Value ?? 0));
                (value, index, reason) = (JsonValue.From((double)detail.Value), detail.VariationIndex, detail.Reason);
                break;
            }

        case "json":
        case "any":
            {
                var detail = client.JsonVariationDetail(flagKey, evaluationContext, fallback);
                (value, index, reason) = (detail.Value ?? JsonValue.Null, detail.VariationIndex, detail.Reason);
                break;
            }

        default:
            context.Response.StatusCode = 400;
            await context.Response.WriteAsync("unknown accessor " + accessor);
            return;
    }

    await Reply(context, Obj(
        ("value", value),
        ("variationIndex", index is { } i ? JsonValue.From(i) : JsonValue.Null),
        ("reason", ReasonJson(reason))), 200);
}

static async Task<JsonObject> ReadBody(HttpContext context)
{
    using var reader = new StreamReader(context.Request.Body, new UTF8Encoding(false), false);
    var text = await reader.ReadToEndAsync();
    if (text.Length == 0)
    {
        return new JsonObject([]);
    }

    return JsonValue.Parse(text) as JsonObject ?? throw new InvalidDataException("the body is not a JSON object");
}

static string? Text(JsonObject body, string name) => body.TryGetValue(name, out var v) && v is JsonString s ? s.Value : null;

static bool Flag(JsonObject body, string name) => body.TryGetValue(name, out var v) && v is JsonBool { Value: true };

static TimeSpan? Millis(JsonObject body, string name) =>
    body.TryGetValue(name, out var v) && v is JsonNumber n ? TimeSpan.FromMilliseconds(n.Value) : null;

static JsonObject Obj(params (string Name, JsonValue Value)[] fields) =>
    new(fields.Select(f => new KeyValuePair<string, JsonValue>(f.Name, f.Value)));

static async Task Reply(HttpContext context, JsonValue body, int status)
{
    context.Response.StatusCode = status;
    context.Response.ContentType = "application/json; charset=utf-8";
    await context.Response.WriteAsync(body.ToJson(), new UTF8Encoding(false));
}

static JsonValue ReasonJson(Reason reason)
{
    var fields = new List<(string, JsonValue)> { ("kind", JsonValue.From(reason.Kind.ToString())) };
    if (reason.RuleIndex is { } rule)
    {
        fields.Add(("ruleIndex", JsonValue.From(rule)));
    }

    if (reason.FlagKey is { } flagKey)
    {
        fields.Add(("flagKey", JsonValue.From(flagKey)));
    }

    if (reason.ErrorCode is { } code)
    {
        fields.Add(("code", JsonValue.From(code.ToString())));
    }

    return Obj([.. fields]);
}

static JsonValue StatusJson(ClientStatus status) => Obj(
    ("etag", JsonValue.From(status.Etag)),
    ("lastSyncAt", status.LastSyncAt is { } at ? JsonValue.From((double)at.ToUnixTimeMilliseconds()) : JsonValue.Null),
    ("stream", JsonValue.From(status.Stream.ToString())),
    ("lastError", JsonValue.From(status.LastError)));

static JsonValue Boxed(object? value) => value switch
{
    null => JsonValue.Null,
    JsonValue json => json,
    bool b => JsonValue.From(b),
    string s => JsonValue.From(s),
    double d => JsonValue.From(d),
    long l => JsonValue.From((double)l),
    _ => JsonValue.From(Convert.ToString(value, CultureInfo.InvariantCulture)),
};

static JsonValue EventJson(EvaluationEvent e) => Obj(
    ("flagKey", JsonValue.From(e.FlagKey)),
    ("context", Boxed(e.Context)),
    ("value", Boxed(e.Value)),
    ("variationIndex", e.VariationIndex is { } i ? JsonValue.From(i) : JsonValue.Null),
    ("reason", ReasonJson(e.Reason)),
    ("etag", JsonValue.From(e.Etag)),
    ("flagVersion", e.FlagVersion is { } v ? JsonValue.From((double)v) : JsonValue.Null));

internal sealed class Entry
{
    public ToggleSlashClient Client { get; set; } = null!;

    public object Lock { get; } = new();

    public List<EvaluationEvent> HookEvents { get; } = [];

    public List<string> Errors { get; } = [];
}
