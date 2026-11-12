using ToggleSlash.Sdk.Evaluation;
using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk;

/// <summary>The options of <see cref="ToggleSlashClient.Create"/>. Everything but <see cref="Url"/> and <see cref="SdkKey"/> has a default.</summary>
public sealed class ClientOptions
{
    /// <summary>The server's base URL. Trailing <c>/</c> characters are removed; nothing else is validated.</summary>
    public string? Url { get; set; }

    /// <summary>The server SDK key, sent as <c>Authorization: Bearer</c>.</summary>
    public string? SdkKey { get; set; }

    /// <summary>Follow <c>GET /sdk/v1/stream</c>. Null (not given) means on; false means poll only.</summary>
    public bool? Stream { get; set; }

    /// <summary>The poll interval (and the ceiling of the backoff). Null, zero or negative means 30 s.</summary>
    public TimeSpan? PollInterval { get; set; }

    /// <summary>The usage flush interval. Null, zero or negative means 30 s.</summary>
    public TimeSpan? FlushInterval { get; set; }

    /// <summary>The limit of one whole request, body included. Null, zero or negative means 10 s.</summary>
    public TimeSpan? RequestTimeout { get; set; }

    /// <summary>Called synchronously on the caller's thread after every evaluation. An exception it throws goes to <see cref="OnError"/>.</summary>
    public Action<EvaluationEvent>? AfterEvaluation { get; set; }

    /// <summary>Like <see cref="AfterEvaluation"/>, for a hook that returns a task. The task is not awaited; a fault goes to <see cref="OnError"/>.</summary>
    public Func<EvaluationEvent, Task>? AfterEvaluationAsync { get; set; }

    /// <summary>Receives every failure the SDK absorbs. An exception it throws is ignored.</summary>
    public Action<Exception>? OnError { get; set; }

    // Test seams (contract section 13). They are internal: the public API has none, and nothing is read from the
    // environment.

    /// <summary>Every wait and timer goes through this.</summary>
    internal TimeProvider? TimeProvider { get; set; }

    /// <summary>Draws the jitter in [0, 1); the wait is the ceiling times (0.5 + 0.5 * draw).</summary>
    internal Func<double>? Jitter { get; set; }

    /// <summary>The HTTP handler (a fake, or a real one with a test certificate). The caller owns it.</summary>
    internal HttpMessageHandler? Handler { get; set; }

    /// <summary>The stream watchdog; 60 s when null.</summary>
    internal TimeSpan? StreamWatchdog { get; set; }

    /// <summary>Replaces the evaluator, so a test can make it raise.</summary>
    internal Func<Ruleset, string, JsonValue?, EvaluationResult>? Evaluator { get; set; }
}
