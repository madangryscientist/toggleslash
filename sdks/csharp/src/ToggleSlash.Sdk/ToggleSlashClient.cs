using ToggleSlash.Sdk.Evaluation;
using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk;

/// <summary>Everything a reader needs, as one immutable value: it is swapped whole, so a reader never sees a mix.</summary>
internal sealed record ClientState(Ruleset? Ruleset, string? Etag, DateTimeOffset? LastSyncAt, StreamState Stream, string? LastError);

/// <summary>
/// The ToggleSlash server SDK client (contract <c>docs/sdk-contract.md</c>). <see cref="Create"/> starts two background
/// tasks, one that follows the server and one that sends usage counts; nothing here throws at a caller, and
/// <c>variation</c>, <c>status</c> never do I/O or wait. Call <see cref="Close"/> (or dispose) to flush the counts.
/// </summary>
public sealed partial class ToggleSlashClient : IDisposable, IAsyncDisposable
{
    private static readonly TimeSpan DefaultInterval = TimeSpan.FromSeconds(30);
    private static readonly TimeSpan DefaultTimeout = TimeSpan.FromSeconds(10);
    private static readonly TimeSpan DefaultWatchdog = TimeSpan.FromSeconds(60);

    // True while this execution context is inside an OnError call, which runs on the SDK's own thread (or the caller's).
    // Close() and Ready() called there must not wait for the work that is calling them.
    private static readonly AsyncLocal<bool> InCallback = new();

    private readonly Action<EvaluationEvent>? _afterEvaluation;
    private readonly Func<EvaluationEvent, Task>? _afterEvaluationAsync;
    private readonly Action<Exception>? _onError;
    private readonly Func<Ruleset, string, JsonValue?, EvaluationResult> _evaluate;
    private readonly TimeProvider _time;
    private readonly Func<double> _jitter;
    private readonly bool _stream;
    private readonly TimeSpan _pollInterval;
    private readonly TimeSpan _flushInterval;
    private readonly TimeSpan _watchdog;
    private readonly Transport _transport;
    private readonly CancellationTokenSource _stop = new();
    private readonly CancellationTokenSource _flushStop = new();
    private readonly TaskCompletionSource<bool> _ready = new(TaskCreationOptions.RunContinuationsAsynchronously);

    private ClientState _state;
    private volatile bool _closing;
    private Task? _closeTask;
    private Task _syncTask = Task.CompletedTask;
    private Task _flushTask = Task.CompletedTask;

    private ToggleSlashClient(ClientOptions options)
    {
        _afterEvaluation = options.AfterEvaluation;
        _afterEvaluationAsync = options.AfterEvaluationAsync;
        _onError = options.OnError;
        _evaluate = options.Evaluator ?? Evaluation.Evaluator.Evaluate;
        _time = options.TimeProvider ?? TimeProvider.System;
        _jitter = options.Jitter ?? Random.Shared.NextDouble;
        _stream = options.Stream ?? true;
        _pollInterval = Positive(options.PollInterval, DefaultInterval);
        _flushInterval = Positive(options.FlushInterval, DefaultInterval);
        _watchdog = Positive(options.StreamWatchdog, DefaultWatchdog);
        var timeout = Positive(options.RequestTimeout, DefaultTimeout);
        var url = (options.Url ?? string.Empty).TrimEnd('/');
        _transport = new Transport(url, options.SdkKey, timeout, _time, options.Handler);
        _state = new ClientState(null, null, null, _stream ? StreamState.connecting : StreamState.off, null);
    }

    private static TimeSpan Positive(TimeSpan? value, TimeSpan fallback) => value is { } v && v > TimeSpan.Zero ? v : fallback;

    /// <summary>
    /// Creates a client and returns it at once: the first load runs in the background and <see cref="ReadyAsync"/> says when
    /// it has finished. It never throws; an unusable <see cref="ClientOptions.Url"/> or key makes every request a
    /// <c>network</c> error.
    /// </summary>
    /// <param name="options">The options; null is the same as no options.</param>
    /// <returns>The running client.</returns>
    public static ToggleSlashClient Create(ClientOptions? options)
    {
        var client = new ToggleSlashClient(options ?? new ClientOptions());
        client.Start();
        return client;
    }

    private void Start()
    {
        _syncTask = Task.Run(SyncLoopAsync);
        _flushTask = Task.Run(FlushLoopAsync);
    }

    // ---- ready ---------------------------------------------------------------------------------------------

    /// <summary>
    /// Completes after the first load attempt (bounded by the request timeout) with whether a ruleset is held. The answer is
    /// fixed then, and is false if <see cref="Close"/> came first. The task never faults.
    /// </summary>
    /// <returns>A task of the answer.</returns>
    public Task<bool> ReadyAsync() => _ready.Task;

    /// <summary>
    /// Blocks until <see cref="ReadyAsync"/> completes. Called from an <see cref="ClientOptions.OnError"/> callback it does not
    /// wait: it answers from the state held.
    /// </summary>
    /// <returns>Whether a ruleset is held.</returns>
    public bool Ready()
    {
        if (InCallback.Value && !_ready.Task.IsCompleted)
        {
            return Volatile.Read(ref _state).Ruleset is not null;
        }

        return _ready.Task.GetAwaiter().GetResult();
    }

    private void SettleReady() => _ready.TrySetResult(Volatile.Read(ref _state).Ruleset is not null);

    // ---- status --------------------------------------------------------------------------------------------

    /// <summary>One consistent snapshot, read from memory.</summary>
    /// <returns>The snapshot.</returns>
    public ClientStatus Status()
    {
        var state = Volatile.Read(ref _state);
        return new ClientStatus(state.Etag, state.LastSyncAt, state.Stream, state.LastError);
    }

    /// <summary>Swaps the state with compare-and-swap. After close began the stream state stays <c>closed</c>.</summary>
    private void Update(Func<ClientState, ClientState> change)
    {
        while (true)
        {
            var current = Volatile.Read(ref _state);
            var next = change(current);
            if (current.Stream == StreamState.closed && next.Stream != StreamState.closed)
            {
                next = next with { Stream = StreamState.closed };
            }

            if (ReferenceEquals(Interlocked.CompareExchange(ref _state, next, current), current))
            {
                return;
            }
        }
    }

    private DateTimeOffset Now()
    {
        var now = _time.GetUtcNow();
        return new DateTimeOffset(now.UtcTicks - (now.UtcTicks % TimeSpan.TicksPerMillisecond), TimeSpan.Zero);
    }

    // ---- errors --------------------------------------------------------------------------------------------

    /// <summary>Passes a failure to <c>OnError</c>. No SDK lock is held; an exception the callback throws is ignored.</summary>
    private void ReportError(Exception error)
    {
        var callback = _onError;
        if (callback is null)
        {
            return;
        }

        var previous = InCallback.Value;
        InCallback.Value = true;
        try
        {
            callback(error);
        }
        catch (Exception)
        {
            // A callback that raises is ignored (contract 10).
        }
        finally
        {
            InCallback.Value = previous;
        }
    }

    // ---- evaluation ----------------------------------------------------------------------------------------

    /// <summary>Evaluates a boolean flag.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context: a <see cref="JsonValue"/> or native values (dictionary, list, string, number, bool).</param>
    /// <param name="fallback">What to return when the flag does not serve a boolean.</param>
    /// <returns>The value or the fallback.</returns>
    public bool BoolVariation(string flagKey, object? context, bool fallback) => Variation(flagKey, context, fallback);

    /// <summary>Evaluates a string flag.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The value or the fallback.</returns>
    public string? StringVariation(string flagKey, object? context, string? fallback) => Variation(flagKey, context, fallback);

    /// <summary>Evaluates an integer flag: a fractional or out-of-int64 served number is a type mismatch.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The value or the fallback.</returns>
    public long IntVariation(string flagKey, object? context, long fallback) => Variation(flagKey, context, fallback);

    /// <summary>Evaluates a number flag.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The value or the fallback.</returns>
    public double FloatVariation(string flagKey, object? context, double fallback) => Variation(flagKey, context, fallback);

    /// <summary>Evaluates a flag of any kind. A null fallback accepts any served value.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The value or the fallback.</returns>
    public JsonValue? JsonVariation(string flagKey, object? context, JsonValue? fallback) => Variation(flagKey, context, fallback);

    /// <summary>
    /// Evaluates a flag and reads it as <typeparamref name="T"/>: <c>bool</c>, <c>string</c>, an integer type (a whole number in
    /// that type's range), <c>float</c>, <c>double</c>, <see cref="JsonValue"/> or the <c>Nullable&lt;&gt;</c> of a value type.
    /// Any other <typeparamref name="T"/> has no JSON kind, so the caller gets the fallback with <c>TYPE_MISMATCH</c>.
    /// </summary>
    /// <typeparam name="T">The static type wanted.</typeparam>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The value or the fallback.</returns>
    public T Variation<T>(string flagKey, object? context, T fallback) => VariationDetail(flagKey, context, fallback).Value;

    /// <summary>Evaluates a boolean flag with its variation index and reason.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The detail.</returns>
    public Detail<bool> BoolVariationDetail(string flagKey, object? context, bool fallback) => VariationDetail(flagKey, context, fallback);

    /// <summary>Evaluates a string flag with its variation index and reason.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The detail.</returns>
    public Detail<string?> StringVariationDetail(string flagKey, object? context, string? fallback) => VariationDetail(flagKey, context, fallback);

    /// <summary>Evaluates an integer flag with its variation index and reason.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The detail.</returns>
    public Detail<long> IntVariationDetail(string flagKey, object? context, long fallback) => VariationDetail(flagKey, context, fallback);

    /// <summary>Evaluates a number flag with its variation index and reason.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The detail.</returns>
    public Detail<double> FloatVariationDetail(string flagKey, object? context, double fallback) => VariationDetail(flagKey, context, fallback);

    /// <summary>Evaluates a flag of any kind with its variation index and reason.</summary>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The detail.</returns>
    public Detail<JsonValue?> JsonVariationDetail(string flagKey, object? context, JsonValue? fallback) => VariationDetail(flagKey, context, fallback);

    /// <summary>The generic form of <c>VariationDetail</c>; see <see cref="Variation{T}"/> for the types read.</summary>
    /// <typeparam name="T">The static type wanted.</typeparam>
    /// <param name="flagKey">The flag.</param>
    /// <param name="context">The context.</param>
    /// <param name="fallback">The fallback.</param>
    /// <returns>The detail.</returns>
    public Detail<T> VariationDetail<T>(string flagKey, object? context, T fallback)
    {
        var state = Volatile.Read(ref _state);
        var value = fallback;
        int? index = null;
        Reason reason;
        Exception? failure = null;
        var counted = false;
        long? flagVersion = null;
        try
        {
            if (state.Ruleset is null)
            {
                reason = Reason.Error(ErrorCode.NOT_READY);
            }
            else
            {
                flagVersion = VersionOf(state.Ruleset, flagKey);
                var result = _evaluate(state.Ruleset, flagKey, ContextConverter.Convert(context));
                if (result.VariationIndex is null)
                {
                    reason = result.Reason;
                }
                else if (!Accessor<T>.TryConvert(result.Value, fallback, out var converted))
                {
                    reason = Reason.Error(ErrorCode.TYPE_MISMATCH);
                }
                else
                {
                    value = converted;
                    index = result.VariationIndex;
                    reason = result.Reason;
                    counted = true;
                }
            }
        }
        catch (Exception e)
        {
            value = fallback;
            index = null;
            counted = false;
            reason = Reason.Error(ErrorCode.EVALUATION_FAILED);
            failure = e;
        }

        if (counted)
        {
            Record(flagKey, index!.Value);
        }

        if (failure is not null)
        {
            ReportError(failure);
        }

        RunHooks(new EvaluationEvent(flagKey, context, value, index, reason, state.Etag, flagVersion));
        return new Detail<T>(value, index, reason);
    }

    private static long? VersionOf(Ruleset ruleset, string flagKey)
    {
        var version = ruleset.VersionOf(flagKey);
        return version is { } v && v == Math.Floor(v) && v >= -9223372036854775808.0 && v < 9223372036854775808.0 ? (long)v : null;
    }

    private void RunHooks(EvaluationEvent evaluation)
    {
        if (_afterEvaluation is { } hook)
        {
            try
            {
                hook(evaluation);
            }
            catch (Exception e)
            {
                ReportError(e);
            }
        }

        if (_afterEvaluationAsync is { } asyncHook)
        {
            try
            {
                var task = asyncHook(evaluation);
                if (task is not null)
                {
                    if (task.IsCompleted)
                    {
                        if (task.IsFaulted)
                        {
                            ReportError(task.Exception!.GetBaseException());
                        }
                    }
                    else
                    {
                        _ = task.ContinueWith(
                            t => ReportError(t.Exception!.GetBaseException()),
                            CancellationToken.None,
                            TaskContinuationOptions.OnlyOnFaulted | TaskContinuationOptions.ExecuteSynchronously,
                            TaskScheduler.Default);
                    }
                }
            }
            catch (Exception e)
            {
                ReportError(e);
            }
        }
    }

    // ---- close ---------------------------------------------------------------------------------------------

    /// <summary>
    /// Stops following the server and the flush loop, waits for them and for a flush already running, then sends the final
    /// usage flush. Safe to call twice or concurrently; every call waits for the same shutdown. Called from an
    /// <see cref="ClientOptions.OnError"/> callback it starts the shutdown and returns without waiting. Evaluation still
    /// answers afterwards, with nothing counted. It returns within about twice the request timeout.
    /// </summary>
    public void Close()
    {
        var task = CloseAsync();
        if (InCallback.Value)
        {
            return;
        }

        try
        {
            task.GetAwaiter().GetResult();
        }
        catch (Exception)
        {
            // CloseAsync never faults; nothing here may throw either.
        }
    }

    /// <summary>The asynchronous <see cref="Close"/>. The task never faults.</summary>
    /// <returns>A task that completes when the shutdown is done.</returns>
    public Task CloseAsync()
    {
        var existing = Volatile.Read(ref _closeTask);
        if (existing is not null)
        {
            return existing;
        }

        var done = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        existing = Interlocked.CompareExchange(ref _closeTask, done.Task, null);
        if (existing is not null)
        {
            return existing;
        }

        // Nothing is counted from here on, and the stream state reads closed at once.
        _closing = true;
        Update(s => s with { Stream = StreamState.closed });
        _ = Task.Run(() => ShutDownAsync(done));
        return done.Task;
    }

    private async Task ShutDownAsync(TaskCompletionSource done)
    {
        try
        {
            await _stop.CancelAsync().ConfigureAwait(false);
            await _flushStop.CancelAsync().ConfigureAwait(false);
            await Task.WhenAll(Quiet(_syncTask), Quiet(_flushTask)).ConfigureAwait(false);
            _ready.TrySetResult(false);
            await FinalFlushAsync().ConfigureAwait(false);
        }
        catch (Exception)
        {
            // Close never fails.
        }
        finally
        {
            _ready.TrySetResult(false);
            done.TrySetResult();
        }
    }

    private static async Task Quiet(Task task)
    {
        try
        {
            await task.ConfigureAwait(false);
        }
        catch (Exception)
        {
            // The loops catch everything themselves; this is the last net.
        }
    }

    /// <inheritdoc />
    public void Dispose() => Close();

    /// <inheritdoc />
    public ValueTask DisposeAsync() => new(CloseAsync());
}
