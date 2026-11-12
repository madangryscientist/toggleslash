using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk;

/// <summary>
/// The stream state of <see cref="ClientStatus"/>. The members are spelled as the contract spells them
/// (<c>off</c>, <c>connecting</c>, <c>connected</c>, <c>disconnected</c>, <c>closed</c>).
/// </summary>
public enum StreamState
{
    /// <summary>The <c>Stream</c> option is off. It stays so until <c>Close</c>.</summary>
    off,

    /// <summary>The first load, and each stream attempt while it is opening.</summary>
    connecting,

    /// <summary>The stream's <c>connected</c> event arrived and the stream is still open.</summary>
    connected,

    /// <summary>The last stream attempt ended and the client is waiting to retry, polling meanwhile.</summary>
    disconnected,

    /// <summary><c>Close</c> has been called.</summary>
    closed,
}

/// <summary>One consistent snapshot of the client. It holds no URL, key, message, context, flag key or count.</summary>
/// <param name="Etag">The ETag of the ruleset held, as received; null before the first load or when the server sent none.</param>
/// <param name="LastSyncAt">When the client last confirmed its ruleset matches the server (whole milliseconds), or null.</param>
/// <param name="Stream">The stream state.</param>
/// <param name="LastError">Why the most recent ruleset or stream attempt failed: <c>http_&lt;status&gt;</c>, <c>timeout</c>,
/// <c>bad_ruleset</c> or <c>network</c>; null when none.</param>
public sealed record ClientStatus(string? Etag, DateTimeOffset? LastSyncAt, StreamState Stream, string? LastError);

/// <summary>The three parts of <c>variationDetail</c>.</summary>
/// <typeparam name="T">The accessor's type.</typeparam>
/// <param name="Value">The served value, or the caller's fallback.</param>
/// <param name="VariationIndex">The index of the served variation; null whenever <paramref name="Value"/> is the fallback.</param>
/// <param name="Reason">Why.</param>
public readonly record struct Detail<T>(T Value, int? VariationIndex, Reason Reason);

/// <summary>What <see cref="ClientOptions.AfterEvaluation"/> is called with after every evaluation.</summary>
/// <param name="FlagKey">The key the caller asked for.</param>
/// <param name="Context">The context object the caller passed (the SDK sends none of it anywhere).</param>
/// <param name="Value">What the caller is about to get, boxed in the accessor's own type (<c>long</c> for the integer
/// accessor, a <see cref="JsonValue"/> for the JSON accessor, the generic call's <c>T</c>).</param>
/// <param name="VariationIndex">As <see cref="Detail{T}"/>; null for a fallback.</param>
/// <param name="Reason">As <see cref="Detail{T}"/>.</param>
/// <param name="Etag">The ETag of the ruleset held at this evaluation; null before the first load.</param>
/// <param name="FlagVersion">The flag's <c>version</c> in that ruleset; null when it has no such flag or the flag is malformed.</param>
public sealed record EvaluationEvent(
    string FlagKey,
    object? Context,
    object? Value,
    int? VariationIndex,
    Reason Reason,
    string? Etag,
    long? FlagVersion);

/// <summary>An error the SDK built itself and passed to <see cref="ClientOptions.OnError"/>. It carries no context data.</summary>
public sealed class ToggleSlashException : Exception
{
    /// <summary>Creates the exception.</summary>
    public ToggleSlashException(string message, string code)
        : base(message) => Code = code;

    /// <summary>The status-style code: <c>http_&lt;status&gt;</c>, <c>timeout</c>, <c>bad_ruleset</c>, <c>network</c> or <c>usage_unsent</c>.</summary>
    public string Code { get; }
}
