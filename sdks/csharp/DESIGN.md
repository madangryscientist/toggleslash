# C# SDK: design

A ToggleSlash server SDK for C#, built from `docs/sdk-authoring/PROMPT.md` against `docs/sdk-contract.md` (the
contract) and `docs/evaluation.md` (the spec). Everything below is about this tree, `sdks/csharp`.

The assembly and NuGet package id is `ToggleSlash.Sdk` and the root namespace is `ToggleSlash.Sdk`: the NuGet
convention puts no host in a package id, so the repository path is not used. Nothing is registered under the name.
The entry class is `ToggleSlashClient`, its options are `ClientOptions`, and `ToggleSlashClient.Create(options)`
starts it.

## Mapping table (contract section 2)

| Name | In C# |
| --- | --- |
| `create` | `ToggleSlashClient.Create(ClientOptions options)`: a static factory that returns a client at once, starts two background tasks (the sync loop and the flush loop) and does not wait for the network. It never throws: a null `options`, or an unusable `Url` or `SdkKey`, gives a client whose every request is a `network` error. |
| `ready` | `Task<bool> ReadyAsync()` and the blocking `bool Ready()`. Both settle after the first load attempt (bounded by `RequestTimeout`), answer whether a ruleset is held, and give the same answer on every later call. The task never faults. After `Close` an unsettled one answers false. `Ready()` called from an `OnError` callback (the SDK's own context) does not wait: it answers from the state held. |
| `variation` | typed `BoolVariation`, `StringVariation`, `IntVariation`, `FloatVariation`, `JsonVariation`, and the generic `T Variation<T>(string flagKey, object? context, T fallback)`. Each returns the value or the fallback and never throws. |
| `variationDetail` | the same names with `Detail` after `Variation`: `BoolVariationDetail`, `StringVariationDetail`, `IntVariationDetail`, `FloatVariationDetail`, `JsonVariationDetail`, and the generic `Detail<T> VariationDetail<T>(...)`. A `Detail<T>` is a `readonly record struct (T Value, int? VariationIndex, Reason Reason)`. `VariationIndex` is null when `Value` is the fallback. A `Reason` is a record `(ReasonKind Kind, int? RuleIndex, string? FlagKey, ErrorCode? ErrorCode)`: `RuleIndex` for `RULE_MATCH`, `FlagKey` (the failed prerequisite) for `PREREQUISITE_FAILED`, `ErrorCode` for `ERROR`, null otherwise. |
| `status` | `ClientStatus Status()`: a record `(string? Etag, DateTimeOffset? LastSyncAt, StreamState Stream, string? LastError)`, one consistent snapshot read from one immutable object. `Etag` and `LastError` are null when there is none; `LastSyncAt` is a point in time truncated to whole milliseconds, or null. |
| `close` | `void Close()` and `Task CloseAsync()`, also `IDisposable.Dispose` and `IAsyncDisposable.DisposeAsync`. Idempotent and safe from any thread, concurrently included: every call waits for the same shutdown. It returns within about twice `RequestTimeout` even when requests hang. Called from an `OnError` callback (the SDK's own context) `Close()` starts the shutdown and returns without waiting for it, so it cannot wait for itself. |
| `url` | `ClientOptions.Url` (string), required. Trailing `/` characters are removed. |
| `sdkKey` | `ClientOptions.SdkKey` (string), required. |
| `stream` | `ClientOptions.Stream` (`bool?`), null means "not given", which is on. An explicit false is off. |
| `pollInterval` | `ClientOptions.PollInterval` (`TimeSpan?`), default 30 s. Null, zero or negative means the default. |
| `flushInterval` | `ClientOptions.FlushInterval` (`TimeSpan?`), default 30 s. Same rule. |
| `requestTimeout` | `ClientOptions.RequestTimeout` (`TimeSpan?`), default 10 s, covering the whole request, body included. Same rule. |
| `hooks.afterEvaluation` | `ClientOptions.AfterEvaluation` (`Action<EvaluationEvent>?`) and `ClientOptions.AfterEvaluationAsync` (`Func<EvaluationEvent, Task>?`), default none. Both are called synchronously on the caller's thread before the call returns. A task the async one returns is not awaited: if it later faults, the fault goes to `OnError`. An `EvaluationEvent` is a record `(string FlagKey, object? Context, object? Value, int? VariationIndex, Reason Reason, string? Etag, long? FlagVersion)`. `Context` is the object the caller passed. `Value` is what the caller got, boxed in the accessor's own type: `bool`, `string`, `double`, `long` for `IntVariation`, the generic call's `T`, and a `JsonValue` for `JsonVariation`. A long above 2^53 is carried exactly (it is a `long`). |
| `onError` | `ClientOptions.OnError` (`Action<Exception>?`), default ignore. |
| `accessor shape` | both: typed methods (`Bool`, `String`, `Int`, `Float`, `Json`, each with a `Detail` twin) and one generic `Variation<T>`/`VariationDetail<T>` over the static type `T`. `IntVariation` returns `long`, the signed 64-bit type. The generic method reads `T`: `bool`, `string`, `sbyte`, `byte`, `short`, `ushort`, `int`, `uint`, `long`, `ulong` (integer accessor rules in that type's range), `float`, `double`, `JsonValue`, and the `Nullable<>` of a value type listed. Any other `T` has no JSON kind: the caller gets the fallback with `TYPE_MISMATCH`. |
| `onError thread` | the thread that saw the failure: a thread-pool thread running the sync loop for ruleset and stream failures, one running the flush loop for usage failures, one running `CloseAsync` for counts lost at close and the final flush, and the caller's own thread for a hook that raised or an evaluation that raised (a task a hook returned that faults later is reported from a thread-pool thread). No SDK lock is held while it runs. |

**The closed sets keep their spelling.** `ReasonKind` is an enum with the members `OFF`, `PREREQUISITE_FAILED`,
`TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`, `ERROR`. `ErrorCode` is an enum with `INVALID_CONTEXT`,
`FLAG_NOT_FOUND`, `SEGMENT_NOT_FOUND`, `MALFORMED_FLAG`, `MALFORMED_SEGMENT`, `PREREQUISITE_CYCLE`,
`PREREQUISITE_DEPTH_EXCEEDED`, `NOT_READY`, `TYPE_MISMATCH`, `EVALUATION_FAILED`. `StreamState` is an enum with the
lower-case members `off`, `connecting`, `connected`, `disconnected`, `closed`. The `.editorconfig` allows these names
(each ignored rule has its reason there). `LastError` is a string holding exactly one of `http_<status>`, `timeout`,
`bad_ruleset`, `network`, or null. Each enum's `ToString()` is the member's name.

**"Not given" versus false or zero.** `Stream` is a `bool?` and each duration a `TimeSpan?`: null is "not given", so
an unset `Stream` is on and an explicit false is off. A non-positive duration is also treated as not given (the
contract leaves it unspecified and recommends the default).

**Test seams** (contract section 13) are `internal` properties of `ClientOptions` that the test project sees through
`InternalsVisibleTo`: a `TimeProvider` (every wait and timer goes through it), a jitter source `Func<double>` (the
backoff multiplies by `0.5 + 0.5 * draw`), an `HttpMessageHandler`, the stream watchdog (60 s), and an evaluator that
raises. The Retry-After cap (300 s) has no seam: a test reads the client's own hold times (`SyncHoldUntil` and
`UsageHoldUntil`, internal) against a fake clock, so the 300 is the real constant. The public API has none of them,
and no global or environment variable is read.

`ToggleSlashException` (a message and a `Code`) is the one exception type the SDK builds. It is only ever handed to
`OnError`, never thrown at a caller, and carries no context, key or flag data.

## Concurrency (contract section 6)

- **One immutable state.** Everything a reader needs is one immutable `ClientState` record (ruleset, ETag,
  `LastSyncAt`, stream state, `LastError`) in one volatile field, replaced by compare-and-swap. The ruleset and its
  ETag change in one swap, so a reader sees the old pair or the new pair. `Variation*` reads the field once and uses
  that snapshot for the evaluation, the hook's `Etag` and its `FlagVersion`. `Status()` reads it once, which makes
  it one consistent snapshot: the end of a stream attempt sets `Stream` and `LastError` in one swap.
- **Tasks started.** `Create` starts two tasks with `Task.Run`: the sync loop (first load, then the loop of 7.3
  including the stream reads) and the flush loop (wait `FlushInterval`, flush, repeat). They run on the thread pool,
  whose threads are background threads, and `HttpClient` starts none of its own that keep a process alive, so a client
  never closed does not keep the process alive (6.6).
- **Locks.** None on the evaluation path. Usage counts go in a dictionary under a `lock` that is held only for an
  in-memory add or a swap of the dictionary (never across the network), so no `Variation*` waits for a request. A
  `SemaphoreSlim` serialises usage flushes (9, "Flushes are serial"): it is held across the network and only a flush
  and `Close`'s final flush take it. `ReadyAsync` waits on a `TaskCompletionSource`, `Status` on nothing.
- **No lock during a callback.** `AfterEvaluation` and `OnError` run with no SDK lock held. The state is swapped
  before `OnError` is called, never inside it.
- **Close.** `CloseAsync` sets a `closing` flag first (nothing is counted after it), cancels the sync loop's token
  (which abandons the in-flight ruleset or stream request and the wait), cancels the flush loop's token (its wait,
  not a flush already running), awaits both tasks, settles `ReadyAsync` false if it had not settled, then does the
  final flush. The shutdown is one `Task` created once with `Interlocked.CompareExchange`, so two closes racing share
  it. An `AsyncLocal` marks execution inside an `OnError` call: `Close()` and `Ready()` there return at once instead
  of waiting for the work that is calling them.
- **Every `TaskCompletionSource` is created with `TaskCreationOptions.RunContinuationsAsynchronously`**, in the SDK,
  the tests and the test service, so completing one never runs its awaiting code inline on a thread that holds a
  callback (the continuation of an `OnError` completing a source would otherwise call `Close()` on that thread).
- **No race detector exists for .NET.** A stress test calls every operation from many threads with `Close` racing
  them, and two closes racing, and fails on any exception, hang or inconsistent snapshot.

## HTTP and SSE

One `HttpClient` over a `SocketsHttpHandler` (automatic decompression off, cookies off, redirects not followed so a
`3xx` is `http_<status>`, the client's own `Timeout` infinite). The headers it adds are `Host` (and `Content-Length`
on a POST) only; the SDK adds `Authorization`, `Accept` (the stream), `If-None-Match` and `Content-Type` (events).

**`RequestTimeout` covers the body.** Each ruleset and events request has one `CancellationTokenSource` made with
`new CancellationTokenSource(timeout, timeProvider)`, linked to the client's stop token, and that one token is passed
to `SendAsync` (with `ResponseHeadersRead`) and to the body read. A server that stalls mid-body cancels it, and the
`OperationCanceledException` is a `timeout` when that source fired and the stop token did not.

**The stream** is opened with `ResponseHeadersRead` under a source bounded by `RequestTimeout` (from the request to the
headers). After that one source per stream is re-armed with `CancelAfter(60 s)` before every read, so any bytes,
a keep-alive comment included, restart the 60 s, and a stream that goes 60 s without a byte is dropped as `timeout`.

**SSE** is parsed by a hand-written WHATWG-style state machine (`Sse/SseParser.cs`): it takes decoded text in any
chunking, treats LF, CRLF and a lone CR as line ends (a CR at the end of a chunk is remembered, so a CRLF split
across reads is one end), drops `:` comments, reads only `event` and `data`, strips one leading space from a value,
joins several `data` lines with LF, drops an event with no `data` line, names an event `message` when none was given
and discards an event incomplete at the end. The bytes are decoded by one `Decoder` kept across reads, so UTF-8 split
across reads does not break.

## Dependencies

Runtime: none. The standard library has HTTP (`System.Net.Http`), SHA-256 (`System.Security.Cryptography`), time
(`TimeProvider`) and everything else. JSON is hand-written (below) because `System.Text.Json` cannot keep a lone
surrogate escape or read the way the spec wants.

Test and lint dependencies: `xunit` 2.9.3, `xunit.runner.visualstudio` and `Microsoft.NET.Test.Sdk` (the test runner,
the same set `apps/server/ToggleSlash.Server.Tests` uses), `Microsoft.Extensions.TimeProvider.Testing` (a
`FakeTimeProvider` for the timer tests). The test service uses only the shared framework (`Microsoft.NET.Sdk.Web`).
Lint is `dotnet format` (whitespace, style and analyzers, verify only) plus the compiler's analyzers with warnings as
errors; no package.

## JSON

`Json/JsonValue.cs` is the SDK's own JSON value type: `JsonNull`, `JsonBool`, `JsonNumber` (a `double`, so numbers
are IEEE-754 binary64: `1e999` reads as Infinity and `9007199254740993` as `9007199254740992`, and `-0.0` is kept),
`JsonString`, `JsonArray` and `JsonObject`. `Json/JsonParser.cs` is a recursive-descent RFC 8259 parser over a
`string`. A C# `string` is UTF-16 code units, so a lone surrogate (`"\ud800"`) is held as it is. A duplicate object
key keeps the last value, a leading byte order mark is skipped, and nesting past 512 levels is refused as a parse
failure (the contract's floor, which the contract says an SDK MAY refuse beyond). A response body is decoded by
`UTF8Encoding` with replacement, which since .NET Core 3.0 replaces each maximal invalid subpart with one U+FFFD,
as the WHATWG Encoding standard does.

The public API accepts a context as a `JsonValue` (which keeps lone surrogates and `1e999`) or as a native value
(`IReadOnlyDictionary<string, object?>` or `IDictionary`, a `string`, a `bool`, a number of any C# numeric type read
as the binary64 of the same value, an `IEnumerable`, null). A native value that has no JSON form makes the
context invalid (`ERROR INVALID_CONTEXT`); nothing throws.

## Evaluator (phase 2)

`Evaluation/` holds the evaluator with no network code: the ruleset loader, the context reader, the clause matcher,
`Pattern` (the spec's dialect, parsed to a program and run as a Pike VM over code points), `SemanticVersion`,
`RfcInstant`, `Bucket` (SHA-256 of the UTF-8 of `key.salt.contextKey` through `SHA256.HashData`, first four bytes
big-endian, mod 100000; a lone surrogate becomes U+FFFD only when hashed, which the UTF-8 encoder's replacement
does) and the evaluator. It uses no `System.Text.RegularExpressions` and no `System.Text.Json`; a test searches the
source for both.

## Minimum runtime and toolchain

- **Minimum: net10.0.** The oldest supported .NET (LTS or STS) that has what the SDK needs was looked up on the run
  date (2026-11-12): .NET 8 and .NET 9 both reached end of support on 2026-11-10, so neither is in support, and
  .NET 10 (LTS) is the oldest that is. The assembly targets `net10.0`.
- **Pinned toolchain: .NET SDK 10.0.404**, the newest LTS release, in `mise.toml`, and in this directory's
  `global.json` with the same number and `rollForward: disable` (the repository root's `global.json` pins another SDK,
  and without a local one `dotnet` here picks the root's).
- **Build tool:** the `dotnet` CLI with a `.slnx` solution; `UseArtifactsOutput` keeps build output under an ignored
  `artifacts/` so `bin/` holds only the three scripts.
- **Test framework: xunit 2.9.3** over VSTest, the one `apps/server/ToggleSlash.Server.Tests` already uses. Test
  classes run in parallel, which is part of the load the timing tests are written to survive.
- **Lint and format: `dotnet format`** (the SDK's own tool; `whitespace`, `style` and `analyzers` subcommands, each
  with `--verify-no-changes`) and the compiler with `AnalysisMode` Recommended, `EnforceCodeStyleInBuild` and
  `TreatWarningsAsErrors`. It is the language's mainstream pair and needs nothing beyond the SDK.
- **Integer accessor:** `IntVariation` uses `long`. The generic method reads the range of the integer type `T`.

## Package layout

```
sdks/csharp/
  mise.toml, global.json, Directory.Build.props, .editorconfig, ToggleSlash.Sdk.slnx
  bin/lint, bin/test, bin/test-service
  src/ToggleSlash.Sdk/          the library: Json/, Evaluation/, Sse/, and the client (ToggleSlashClient.cs,
                                ClientOptions.cs, Sync.cs, Usage.cs, Http.cs, Types.cs)
  tests/ToggleSlash.Sdk.Tests/  xunit: the vectors, the native tests, a loopback fake server (Support/)
  tests/ToggleSlash.Sdk.NeverClosed/  a program that leaves a client open and returns; one test checks it exits
  test-service/                 the conformance test service (Microsoft.NET.Sdk.Web), README.md
  DESIGN.md, README.md, RUN.md
```

The evaluator is `internal` to the library (tests see it through `InternalsVisibleTo`); the client, the options,
the result types and `JsonValue` are public. The test service is one more project in the solution, so `bin/test`
builds it last, and `bin/test-service` runs the built output and builds it only when it is missing.
