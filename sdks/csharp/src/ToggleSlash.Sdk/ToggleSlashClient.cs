using System.Text.Json;
using Eval = ToggleSlash.Sdk.Evaluation;

namespace ToggleSlash.Sdk;

/// <summary>
/// A ToggleSlash server SDK client. It keeps a ruleset current in the background, evaluates flags locally, and
/// reports usage counts. Nothing it does throws at the caller; every failure goes to
/// <see cref="ClientOptions.OnError"/>. A client that is never closed does not keep the process alive.
/// </summary>
public sealed class ToggleSlashClient : IDisposable, IAsyncDisposable
{
    // Set inside the SDK's own loops and shutdown, so a Close called from a callback there does not wait for itself.
    private static readonly AsyncLocal<bool> InsideSdk = new();

    private readonly StateHolder _state;
    private readonly UsageReporter? _usage;
    private readonly HttpClient? _http;
    private readonly TimeProvider _time;
    private readonly Action<Exception>? _onError;
    private readonly Action<EvaluationEvent>? _afterEvaluation;
    private readonly Func<Eval.Ruleset, string, JsonElement, Eval.EvaluationResult> _evaluate;
    private readonly CancellationTokenSource _lifetime = new();
    private readonly TaskCompletionSource<bool> _ready = new(TaskCreationOptions.RunContinuationsAsynchronously);
    private readonly Lazy<Task> _shutdown;
    private readonly Task _syncLoop;
    private readonly Task _flushLoop;
    private volatile bool _closing;

    private ToggleSlashClient(ClientOptions options)
    {
        _time = options.Time ?? TimeProvider.System;
        _onError = options.OnError;
        _afterEvaluation = options.Hooks?.AfterEvaluation;
        _evaluate = options.Evaluate ?? Eval.Evaluator.Evaluate;
        _shutdown = new Lazy<Task>(() => Task.Run(ShutDownAsync), LazyThreadSafetyMode.ExecutionAndPublication);

        var stream = options.Stream;
        _state = new StateHolder(new SyncState(null, null, null, stream ? StreamState.Connecting : StreamState.Off, null));
        var problem = Problem(options);
        if (problem is not null)
        {
            // Nothing is requested: the first load fails with this, `ready` answers false and the client waits for close.
            _syncLoop = Task.Run(() => Reject(problem));
            _flushLoop = Task.CompletedTask;
            return;
        }

        var url = options.Url!.TrimEnd('/');
        var requestTimeout = Positive(options.RequestTimeout, Defaults.RequestTimeout);
        _http = Http.CreateClient(options.Handler);
        _usage = new UsageReporter(url, options.SdkKey!, _http, _time, requestTimeout, Report);
        var sync = new RulesetSync(
            new SyncOptions
            {
                BaseUrl = url,
                SdkKey = options.SdkKey!,
                Http = _http,
                Time = _time,
                Jitter = options.Jitter ?? Random.Shared.NextDouble,
                Stream = stream,
                PollInterval = Positive(options.PollInterval, Defaults.Interval),
                RequestTimeout = requestTimeout,
                StreamIdle = options.StreamIdleTimeout ?? Defaults.StreamIdle,
                OnError = Report,
            },
            _state,
            _lifetime.Token);
        var flushInterval = Positive(options.FlushInterval, Defaults.Interval);
        _syncLoop = Task.Run(() => RunSyncAsync(sync));
        _flushLoop = Task.Run(() => RunFlushAsync(flushInterval));
    }

    /// <summary>
    /// Creates a client and starts loading its ruleset in the background. It does not wait for the network
    /// and never throws: bad options are reported through <see cref="ClientOptions.OnError"/> and
    /// <see cref="Status"/>, and <see cref="ReadyAsync"/> answers false.
    /// </summary>
    /// <param name="options">The server URL, the SDK key and the rest.</param>
    /// <returns>The client.</returns>
    public static ToggleSlashClient Create(ClientOptions options) => new(options ?? new ClientOptions());

    private static TimeSpan Positive(TimeSpan value, TimeSpan fallback) => value > TimeSpan.Zero ? value : fallback;

    private static string? Problem(ClientOptions options)
    {
        if (string.IsNullOrWhiteSpace(options.SdkKey)) return "ToggleSlash: SdkKey is required";
        if (!Uri.TryCreate(options.Url, UriKind.Absolute, out var uri) || uri.Scheme is not ("http" or "https"))
            return "ToggleSlash: Url must be an absolute http or https URL";
        return null;
    }

    /// <summary>Completes after the first load attempt: true if a ruleset loaded, false if not. It never faults.</summary>
    /// <returns>A task whose answer is fixed by the first attempt; <see cref="CloseAsync"/> completes an unsettled one false.</returns>
    public Task<bool> ReadyAsync() => _ready.Task;

    /// <summary>What the client has loaded and how its stream is doing, as one snapshot.</summary>
    public ClientStatus Status
    {
        get
        {
            var s = _state.Current;
            return new ClientStatus(s.Etag, s.LastSyncAt, s.Stream, s.LastError);
        }
    }

    /// <summary>Calls the <c>OnError</c> option's callback, ignoring anything it throws.</summary>
    private void Report(Exception error)
    {
        try
        {
            _onError?.Invoke(error);
        }
        catch (Exception)
        {
            // A faulty error callback must not break the SDK.
        }
    }

    private async Task Reject(string problem)
    {
        InsideSdk.Value = true;
        _state.Update(s => s with { LastError = LastErrorCodes.Network });
        Report(new ArgumentException(problem));
        _ready.TrySetResult(false);
        try
        {
            await Task.Delay(Timeout.InfiniteTimeSpan, _lifetime.Token).ConfigureAwait(false);
        }
        catch (OperationCanceledException)
        {
            // Closing.
        }
    }

    private async Task RunSyncAsync(RulesetSync sync)
    {
        InsideSdk.Value = true;
        try
        {
            await sync.RunAsync(() => _ready.TrySetResult(_state.Current.Ruleset is not null)).ConfigureAwait(false);
        }
        catch (Exception error)
        {
            Report(error);
        }
        finally
        {
            _ready.TrySetResult(_state.Current.Ruleset is not null);
        }
    }

    private async Task RunFlushAsync(TimeSpan interval)
    {
        InsideSdk.Value = true;
        var token = _lifetime.Token;
        while (!token.IsCancellationRequested)
        {
            try
            {
                await Task.Delay(interval, _time, token).ConfigureAwait(false);
            }
            catch (OperationCanceledException)
            {
                return;
            }
            await _usage!.FlushAsync().ConfigureAwait(false);
        }
    }

    // ---- evaluation ----

    /// <summary>The bool value of a flag, or <paramref name="fallback"/>.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The served value or the fallback.</returns>
    public bool BoolVariation(string flagKey, Context context, bool fallback) =>
        BoolVariationDetail(flagKey, context, fallback).Value;

    /// <summary>The bool value of a flag with how it was reached.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The value, its variation index and the reason.</returns>
    public VariationDetail<bool> BoolVariationDetail(string flagKey, Context context, bool fallback) =>
        Detail(flagKey, context, fallback, Accessors.Bool);

    /// <summary>The string value of a flag, or <paramref name="fallback"/>.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The served value or the fallback.</returns>
    public string StringVariation(string flagKey, Context context, string fallback) =>
        StringVariationDetail(flagKey, context, fallback).Value;

    /// <summary>The string value of a flag with how it was reached.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The value, its variation index and the reason.</returns>
    public VariationDetail<string> StringVariationDetail(string flagKey, Context context, string fallback) =>
        Detail(flagKey, context, fallback, Accessors.String);

    /// <summary>The integer value of a flag, or <paramref name="fallback"/>. A fractional number or one outside 64 bits is a mismatch.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The served value or the fallback.</returns>
    public long IntVariation(string flagKey, Context context, long fallback) =>
        IntVariationDetail(flagKey, context, fallback).Value;

    /// <summary>The integer value of a flag with how it was reached.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The value, its variation index and the reason.</returns>
    public VariationDetail<long> IntVariationDetail(string flagKey, Context context, long fallback) =>
        Detail(flagKey, context, fallback, Accessors.Int);

    /// <summary>The number value of a flag, or <paramref name="fallback"/>.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The served value or the fallback.</returns>
    public double DoubleVariation(string flagKey, Context context, double fallback) =>
        DoubleVariationDetail(flagKey, context, fallback).Value;

    /// <summary>The number value of a flag with how it was reached.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The value, its variation index and the reason.</returns>
    public VariationDetail<double> DoubleVariationDetail(string flagKey, Context context, double fallback) =>
        Detail(flagKey, context, fallback, Accessors.Double);

    /// <summary>
    /// The JSON value of a flag, or <paramref name="fallback"/>. The served value must be of the fallback's JSON kind;
    /// a fallback of JSON <c>null</c> (or a default one) accepts any value.
    /// </summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The served value or the fallback.</returns>
    public JsonElement JsonVariation(string flagKey, Context context, JsonElement fallback) =>
        JsonVariationDetail(flagKey, context, fallback).Value;

    /// <summary>The JSON value of a flag with how it was reached.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">Who it is for.</param>
    /// <param name="fallback">What to return when no value can be served.</param>
    /// <returns>The value, its variation index and the reason.</returns>
    public VariationDetail<JsonElement> JsonVariationDetail(string flagKey, Context context, JsonElement fallback) =>
        Detail(flagKey, context, fallback, Accessors.Json);

    private VariationDetail<T> Detail<T>(string flagKey, Context context, T fallback, Accessor<T> accessor)
    {
        var snapshot = _state.Current;
        var ruleset = snapshot.Ruleset;
        VariationDetail<T> detail;
        JsonElement? served = null;
        try
        {
            (detail, served) = Compute(ruleset, flagKey, context, fallback, accessor);
        }
        catch (Exception error)
        {
            Report(error);
            detail = Fallback(fallback, ErrorCode.EvaluationFailed);
        }
        if (_afterEvaluation is not null)
        {
            RunHook(new EvaluationEvent(flagKey, context, JsonOf(detail, served, accessor), detail.VariationIndex, detail.Reason, snapshot.Etag, ruleset?.VersionOf(flagKey)));
        }
        return detail;
    }

    private static JsonElement JsonOf<T>(VariationDetail<T> detail, JsonElement? served, Accessor<T> accessor)
    {
        try
        {
            return served ?? accessor.ToJson(detail.Value);
        }
        catch (Exception)
        {
            return Eval.EvaluationResult.JsonNull;
        }
    }

    private static VariationDetail<T> Fallback<T>(T fallback, ErrorCode code) =>
        new(fallback, null, new Reason(ReasonKind.Error, code));

    private (VariationDetail<T> Detail, JsonElement? Served) Compute<T>(
        Eval.Ruleset? ruleset, string flagKey, Context context, T fallback, Accessor<T> accessor)
    {
        if (ruleset is null) return (Fallback(fallback, ErrorCode.NotReady), null);
        var result = _evaluate(ruleset, flagKey, context.Json);
        var reason = ToReason(result.Reason);
        if (result.VariationIndex is not { } index) return (new VariationDetail<T>(fallback, null, reason), null);
        if (!accessor.TryConvert(result.Value, fallback, out var value)) return (Fallback(fallback, ErrorCode.TypeMismatch), null);
        if (!_closing) _usage?.Record(flagKey, index);
        return (new VariationDetail<T>(value, index, reason), result.Value);
    }

    private static Reason ToReason(Eval.Reason reason) =>
        new(
            ReasonKind.FromName(reason.KindName),
            reason.CodeName is { } code ? ErrorCode.FromName(code) : null,
            reason.RuleIndex,
            reason.FlagKey);

    private void RunHook(EvaluationEvent hookEvent)
    {
        try
        {
            _afterEvaluation!(hookEvent);
        }
        catch (Exception error)
        {
            Report(error);
        }
    }

    // ---- close ----

    /// <summary>
    /// Stops the background work, sends the last usage counts and waits for both. Safe to call more than once and
    /// from several threads; every call returns when the one shutdown is done. Called from inside one of the
    /// SDK's own callbacks (<see cref="ClientOptions.OnError"/>), it starts the shutdown and returns without
    /// waiting for it.
    /// </summary>
    public void Close()
    {
        var task = CloseAsync();
        if (InsideSdk.Value) return;
        task.GetAwaiter().GetResult();
    }

    /// <summary>The asynchronous <see cref="Close"/>.</summary>
    /// <returns>A task that completes when the shutdown is done.</returns>
    public Task CloseAsync()
    {
        // The first part is synchronous, so nothing is counted and no ready answer is left open once this returns.
        _closing = true;
        _state.Close();
        _ready.TrySetResult(false);
        try
        {
            _lifetime.Cancel();
        }
        catch (Exception error)
        {
            Report(error);
        }
        return _shutdown.Value;
    }

    private async Task ShutDownAsync()
    {
        InsideSdk.Value = true;
        try
        {
            await Task.WhenAll(_syncLoop, _flushLoop).ConfigureAwait(false);
            if (_usage is not null) await _usage.FinishAsync().ConfigureAwait(false);
        }
        catch (Exception error)
        {
            Report(error);
        }
        finally
        {
            _usage?.Dispose();
            _http?.Dispose();
        }
    }

    /// <inheritdoc />
    public void Dispose() => Close();

    /// <inheritdoc />
    public async ValueTask DisposeAsync() => await CloseAsync().ConfigureAwait(false);
}

/// <summary>How one typed accessor reads a served JSON value, and writes its fallback back as JSON for a hook.</summary>
internal sealed class Accessor<T>(TryConvert<T> convert, Func<T, JsonElement> toJson)
{
    public bool TryConvert(JsonElement served, T fallback, out T value) => convert(served, fallback, out value);

    public JsonElement ToJson(T value) => toJson(value);
}

internal delegate bool TryConvert<T>(JsonElement served, T fallback, out T value);

internal static class Accessors
{
    public static readonly Accessor<bool> Bool = new(
        (JsonElement served, bool _, out bool value) =>
        {
            value = served.ValueKind == JsonValueKind.True;
            return served.ValueKind is JsonValueKind.True or JsonValueKind.False;
        },
        value => JsonSerializer.SerializeToElement(value));

    public static readonly Accessor<string> String = new(
        (JsonElement served, string _, out string value) =>
        {
            value = served.ValueKind == JsonValueKind.String ? Eval.JsonStrings.Read(served) : "";
            return served.ValueKind == JsonValueKind.String;
        },
        value => JsonSerializer.SerializeToElement(value));

    public static readonly Accessor<double> Double = new(
        (JsonElement served, double _, out double value) => Eval.Validation.TryNumber(served, out value),
        value => double.IsFinite(value) ? JsonSerializer.SerializeToElement(value) : Eval.EvaluationResult.JsonNull);

    public static readonly Accessor<long> Int = new(
        (JsonElement served, long _, out long value) =>
        {
            value = 0;
            if (!Eval.Validation.TryNumber(served, out var number)) return false;
            // 2^63 is the first double past long.MaxValue, and -2^63 is long.MinValue exactly.
            if (!double.IsFinite(number) || number != Math.Floor(number) || number < -9223372036854775808.0 || number >= 9223372036854775808.0) return false;
            value = (long)number;
            return true;
        },
        value => JsonSerializer.SerializeToElement(value));

    public static readonly Accessor<JsonElement> Json = new(
        (JsonElement served, JsonElement fallback, out JsonElement value) =>
        {
            value = served;
            var wanted = fallback.ValueKind;
            if (wanted is JsonValueKind.Undefined or JsonValueKind.Null) return true;
            return Kind(served.ValueKind) == Kind(wanted);
        },
        value => value.ValueKind == JsonValueKind.Undefined ? Eval.EvaluationResult.JsonNull : value);

    private static JsonValueKind Kind(JsonValueKind kind) => kind == JsonValueKind.False ? JsonValueKind.True : kind;
}
