using System.Text.Json;

namespace ToggleSlash.Sdk;

/// <summary>Why a flag evaluated the way it did. The names are the ones in docs/evaluation.md.</summary>
public readonly record struct ReasonKind
{
    private ReasonKind(string name) => Name = name;

    internal static ReasonKind FromName(string name) => new(name);

    /// <summary>The canonical spelling, for example <c>RULE_MATCH</c>.</summary>
    public string Name { get; }

    /// <summary>The flag is off.</summary>
    public static readonly ReasonKind Off = new("OFF");
    /// <summary>A target named the context.</summary>
    public static readonly ReasonKind TargetMatch = new("TARGET_MATCH");
    /// <summary>A rule matched.</summary>
    public static readonly ReasonKind RuleMatch = new("RULE_MATCH");
    /// <summary>No rule matched, so the fallthrough served.</summary>
    public static readonly ReasonKind Fallthrough = new("FALLTHROUGH");
    /// <summary>A prerequisite flag did not serve the required variation.</summary>
    public static readonly ReasonKind PrerequisiteFailed = new("PREREQUISITE_FAILED");
    /// <summary>The flag could not be evaluated; <see cref="Reason.Code"/> says why.</summary>
    public static readonly ReasonKind Error = new("ERROR");

    /// <inheritdoc />
    public override string ToString() => Name;
}

/// <summary>Why an evaluation is an error. The names are the ones in docs/sdk-contract.md.</summary>
public readonly record struct ErrorCode
{
    private ErrorCode(string name) => Name = name;

    internal static ErrorCode FromName(string name) => new(name);

    /// <summary>The canonical spelling, for example <c>FLAG_NOT_FOUND</c>.</summary>
    public string Name { get; }

    /// <summary>No ruleset has loaded yet.</summary>
    public static readonly ErrorCode NotReady = new("NOT_READY");
    /// <summary>The flag exists but its value is not of the fallback's kind.</summary>
    public static readonly ErrorCode TypeMismatch = new("TYPE_MISMATCH");
    /// <summary>The evaluation itself raised.</summary>
    public static readonly ErrorCode EvaluationFailed = new("EVALUATION_FAILED");
    /// <summary>The context is not a valid context.</summary>
    public static readonly ErrorCode InvalidContext = new("INVALID_CONTEXT");
    /// <summary>The ruleset has no such flag.</summary>
    public static readonly ErrorCode FlagNotFound = new("FLAG_NOT_FOUND");
    /// <summary>A rule names a segment the ruleset does not have.</summary>
    public static readonly ErrorCode SegmentNotFound = new("SEGMENT_NOT_FOUND");
    /// <summary>The flag is not valid.</summary>
    public static readonly ErrorCode MalformedFlag = new("MALFORMED_FLAG");
    /// <summary>A segment the flag uses is not valid.</summary>
    public static readonly ErrorCode MalformedSegment = new("MALFORMED_SEGMENT");
    /// <summary>The prerequisites form a cycle.</summary>
    public static readonly ErrorCode PrerequisiteCycle = new("PREREQUISITE_CYCLE");
    /// <summary>The prerequisites nest too deeply.</summary>
    public static readonly ErrorCode PrerequisiteDepthExceeded = new("PREREQUISITE_DEPTH_EXCEEDED");

    /// <inheritdoc />
    public override string ToString() => Name;
}

/// <summary>The state of the live update stream, as <see cref="ClientStatus.Stream"/> reports it.</summary>
public readonly record struct StreamState
{
    private StreamState(string name) => Name = name;

    /// <summary>The canonical spelling, for example <c>connected</c>.</summary>
    public string Name { get; }

    /// <summary><see cref="ClientOptions.Stream"/> is off.</summary>
    public static readonly StreamState Off = new("off");
    /// <summary>The first load, or a stream attempt that is opening.</summary>
    public static readonly StreamState Connecting = new("connecting");
    /// <summary>The server's <c>connected</c> event arrived and the stream is open.</summary>
    public static readonly StreamState Connected = new("connected");
    /// <summary>The last stream attempt ended and the SDK is waiting to retry.</summary>
    public static readonly StreamState Disconnected = new("disconnected");
    /// <summary>After <c>Close</c>.</summary>
    public static readonly StreamState Closed = new("closed");

    /// <inheritdoc />
    public override string ToString() => Name;
}

/// <summary>The values of <see cref="ClientStatus.LastError"/> that are not <c>http_&lt;status&gt;</c>.</summary>
public static class LastErrorCodes
{
    /// <summary>The request ran past <see cref="ClientOptions.RequestTimeout"/>, or the stream went quiet.</summary>
    public const string Timeout = "timeout";
    /// <summary>A 200 whose body is not a ruleset.</summary>
    public const string BadRuleset = "bad_ruleset";
    /// <summary>The connection failed, or any other failure.</summary>
    public const string Network = "network";
}

/// <summary>The reason half of a result.</summary>
/// <param name="Kind">What kind of answer this is.</param>
/// <param name="Code">Set for <see cref="ReasonKind.Error"/>.</param>
/// <param name="RuleIndex">Set for <see cref="ReasonKind.RuleMatch"/>.</param>
/// <param name="FlagKey">Set for <see cref="ReasonKind.PrerequisiteFailed"/>.</param>
public sealed record Reason(ReasonKind Kind, ErrorCode? Code = null, int? RuleIndex = null, string? FlagKey = null);

/// <summary>A value with how it was reached.</summary>
/// <typeparam name="T">The kind of value the caller asked for.</typeparam>
/// <param name="Value">The served value, or the fallback when none could be served.</param>
/// <param name="VariationIndex">The served variation, or null when the fallback was returned.</param>
/// <param name="Reason">Why.</param>
public sealed record VariationDetail<T>(T Value, int? VariationIndex, Reason Reason);

/// <summary>A snapshot of what the SDK has loaded. Local state only; the server never sees it.</summary>
/// <param name="Etag">The ETag of the ruleset held, as received; null before the first load.</param>
/// <param name="LastSyncAt">When the ruleset was last confirmed current; null if never.</param>
/// <param name="Stream">The state of the update stream.</param>
/// <param name="LastError"><c>http_&lt;status&gt;</c> or one of <see cref="LastErrorCodes"/>; null if the last attempt did not fail.</param>
public sealed record ClientStatus(string? Etag, DateTimeOffset? LastSyncAt, StreamState Stream, string? LastError);

/// <summary>What <see cref="Hooks.AfterEvaluation"/> is told after each evaluation.</summary>
/// <param name="FlagKey">The flag asked for.</param>
/// <param name="Context">The context it was asked for.</param>
/// <param name="Value">The value returned: the served one, or the fallback.</param>
/// <param name="VariationIndex">The served variation, or null.</param>
/// <param name="Reason">Why.</param>
/// <param name="Etag">The ETag of the ruleset that answered; null before the first load.</param>
/// <param name="FlagVersion">The flag's version in that ruleset; null when the flag is missing or malformed.</param>
public sealed record EvaluationEvent(
    string FlagKey,
    Context Context,
    JsonElement Value,
    int? VariationIndex,
    Reason Reason,
    string? Etag,
    double? FlagVersion);

/// <summary>Callbacks the SDK makes around an evaluation.</summary>
public sealed record Hooks
{
    /// <summary>
    /// Runs on the calling thread, synchronously, after each evaluation and before the call returns.
    /// An exception it throws goes to <see cref="ClientOptions.OnError"/>.
    /// </summary>
    public Action<EvaluationEvent>? AfterEvaluation { get; init; }
}

/// <summary>Options for <see cref="ToggleSlashClient.Create(ClientOptions)"/>.</summary>
public sealed class ClientOptions
{
    /// <summary>The ToggleSlash server's base URL. Required.</summary>
    public string? Url { get; init; }

    /// <summary>The server SDK key. Required.</summary>
    public string? SdkKey { get; init; }

    /// <summary>Follow the update stream; when false, only poll. Default true.</summary>
    public bool Stream { get; init; } = true;

    /// <summary>The poll interval, and the cap of the stream's retry backoff. Default 30 s; zero or less means the default.</summary>
    public TimeSpan PollInterval { get; init; } = Defaults.Interval;

    /// <summary>How often usage counts are sent. Default 30 s; zero or less means the default.</summary>
    public TimeSpan FlushInterval { get; init; } = Defaults.Interval;

    /// <summary>The longest one request may take, body included. Default 10 s; zero or less means the default.</summary>
    public TimeSpan RequestTimeout { get; init; } = Defaults.RequestTimeout;

    /// <summary>Callbacks around evaluation.</summary>
    public Hooks? Hooks { get; init; }

    /// <summary>Told about every failure the SDK absorbs. An exception it throws is ignored.</summary>
    public Action<Exception>? OnError { get; init; }

    // Test seams. Internal: the tests see them through InternalsVisibleTo, and no caller can set them.
    internal HttpMessageHandler? Handler { get; init; }
    internal TimeProvider? Time { get; init; }
    internal Func<double>? Jitter { get; init; }
    internal TimeSpan? StreamIdleTimeout { get; init; }
    internal Func<Evaluation.Ruleset, string, JsonElement, Evaluation.EvaluationResult>? Evaluate { get; init; }
}

internal static class Defaults
{
    public static readonly TimeSpan Interval = TimeSpan.FromSeconds(30);
    public static readonly TimeSpan RequestTimeout = TimeSpan.FromSeconds(10);
    public static readonly TimeSpan StreamIdle = TimeSpan.FromSeconds(60);
}

/// <summary>An evaluation context: one kind, or several, as JSON.</summary>
public readonly struct Context
{
    private readonly JsonElement _element;

    private Context(JsonElement element) => _element = element;

    /// <summary>The context as JSON. A <c>default</c> context reads as JSON null, which is not valid.</summary>
    public JsonElement Json => _element.ValueKind == JsonValueKind.Undefined ? Evaluation.EvaluationResult.JsonNull : _element;

    /// <summary>Wraps a JSON value without changing it, so numbers and strings reach the evaluator as they came.</summary>
    public static implicit operator Context(JsonElement element) => new(element);

    /// <summary>Wraps a JSON value without changing it.</summary>
    public static Context FromElement(JsonElement element) => new(element);

    /// <summary>Parses a context from JSON text. Text that is not JSON gives an invalid context.</summary>
    public static Context FromJson(string json)
    {
        try
        {
            using var document = JsonDocument.Parse(json, Evaluation.JsonText.Options);
            return new Context(document.RootElement.Clone());
        }
        catch (JsonException)
        {
            return default;
        }
    }

    /// <summary>A single-kind context. <paramref name="attributes"/> is a JSON object, or null for none.</summary>
    public static Context ForKey(string key, string? kind = null, IReadOnlyDictionary<string, object?>? attributes = null) =>
        new(JsonSerializer.SerializeToElement(Fields(key, kind, attributes)));

    /// <summary>A multi-kind context from single-kind ones, each built with <see cref="ForKey"/>.</summary>
    public static Context Multi(params (string Kind, Context Context)[] contexts)
    {
        var inner = new Dictionary<string, JsonElement>(StringComparer.Ordinal);
        foreach (var (kind, context) in contexts) inner[kind] = context.Json;
        return new(JsonSerializer.SerializeToElement(new Dictionary<string, object?> { ["kind"] = "multi", ["contexts"] = inner }));
    }

    private static Dictionary<string, object?> Fields(string key, string? kind, IReadOnlyDictionary<string, object?>? attributes)
    {
        var fields = new Dictionary<string, object?>(StringComparer.Ordinal) { ["key"] = key };
        if (kind is not null) fields["kind"] = kind;
        if (attributes is not null) fields["attributes"] = attributes;
        return fields;
    }
}
