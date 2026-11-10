# C# SDK: design

A ToggleSlash server SDK for C#, written to [`docs/sdk-contract.md`](../../docs/sdk-contract.md) (the
contract) and evaluating to [`docs/evaluation.md`](../../docs/evaluation.md) (the spec). Section numbers
below are the contract's. The package is `ToggleSlash.Sdk`; nothing is published or registered under it.

## Input and decisions that follow from it

The brief gave a language and nothing else, so each of these is a decision, not a guess:

- **Runtime:** .NET (CoreCLR), the language's mainstream runtime. No Mono, no .NET Framework.
- **Pinned toolchain:** .NET SDK **10.0.403** in `mise.toml`, the newest SDK of .NET 10, which is the newest
  LTS (supported to 2028-11-14). `global.json` in this directory pins the same number with
  `rollForward: disable`, so `dotnet` here never picks the repository root's `10.0.402`.
- **Minimum version:** **net10.0**. .NET 8 and .NET 9 both reach end of support on 2026-11-10, the day of
  this run, so no older release is clearly in security support; .NET 10 is the only one that certainly is,
  and it has everything the SDK uses (`TimeProvider` with `Task.Delay`, `SocketsHttpHandler`,
  `System.Text.Json`, `SHA256.HashData`, `Lock`). The SDK declares it as `TargetFramework` in
  `src/ToggleSlash.Sdk/ToggleSlash.Sdk.csproj`.
- **Package manager / build tool:** NuGet and the `dotnet` CLI with an SDK-style solution
  (`ToggleSlash.Sdk.slnx`), the mainstream choice. Central package management is not used: three projects
  do not need it.

## 1. The mapping table

| Name | In C# |
| --- | --- |
| `create` | `ToggleSlashClient.Create(ClientOptions options)`, a static factory. It returns a client and never throws (a missing `Url` or `SdkKey`, or a URL that does not parse, is an error the first load reports through `onError` and `status`). It starts the loops on thread pool tasks and does not wait for the network. |
| `ready` | `Task<bool> ReadyAsync()`. Completes `true` or `false` after the first load attempt; the task never faults and never cancels; the answer is fixed by the first attempt. `close` completes an unsettled one `false`. |
| `variation` | Typed methods that return the value: `BoolVariation`, `StringVariation`, `IntVariation` (`long`), `DoubleVariation` and `JsonVariation` (a `JsonElement`, any kind), each `(string flagKey, Context context, T fallback)`. |
| `variationDetail` | The same five names with a `Detail` suffix (`BoolVariationDetail`, ..., `JsonVariationDetail`), each returning `VariationDetail<T>` with `Value`, `VariationIndex` (`int?`) and `Reason`. |
| `status` | `ClientStatus Status { get; }`, a property that returns one immutable snapshot: `Etag` (`string?`), `LastSyncAt` (`DateTimeOffset?`), `Stream` (`StreamState`), `LastError` (`string?`). |
| `close` | `void Close()` and `Task CloseAsync()`. `IDisposable.Dispose` and `IAsyncDisposable.DisposeAsync` call them. Idempotent and safe to call concurrently; every call returns when the one shutdown is done. |
| `url` | `ClientOptions.Url` (`string`, required). Trailing `/` characters are removed. |
| `sdkKey` | `ClientOptions.SdkKey` (`string`, required). |
| `stream` | `ClientOptions.Stream` (`bool`, default `true`). |
| `pollInterval` | `ClientOptions.PollInterval` (`TimeSpan`, default 30 s). |
| `flushInterval` | `ClientOptions.FlushInterval` (`TimeSpan`, default 30 s). |
| `requestTimeout` | `ClientOptions.RequestTimeout` (`TimeSpan`, default 10 s). |
| `hooks.afterEvaluation` | `ClientOptions.Hooks` (a `Hooks` record) with `Hooks.AfterEvaluation` (`Action<EvaluationEvent>?`, default none). |
| `onError` | `ClientOptions.OnError` (`Action<Exception>?`, default ignore). |
| `accessor shape` | Both. Typed methods for `bool`, `string`, `long` (the integer accessor), `double` (the number accessor) and `JsonElement` (`JsonVariation`: the fallback's own JSON kind decides, a `null` fallback accepts any value, so it is also the "any" accessor). Each has a `Detail` twin. There is no generic `Variation<T>`: C# cannot choose by the fallback's runtime type without reflection, and the five typed methods cover every JSON kind. |
| `onError thread` | A failed ruleset, stream or usage request: a thread pool thread of the SDK's own loop, no SDK lock held. A hook that raised, an evaluation that raised (`EVALUATION_FAILED`), and counts lost at `close`: the caller's thread for the first two (inside the `variation` call, no SDK lock held), a thread pool thread of the SDK's own shutdown for the third (also when it was `close` that started it). `onError` can call back into the client. |

**Closed sets.** `ReasonKind`, `ErrorCode` and `StreamState` are `readonly record struct` types around a
string, with a `static readonly` member per value (`ReasonKind.PrerequisiteFailed`, `ErrorCode.NotReady`,
`StreamState.Disconnected`). Their `ToString()` and `Name` are the canonical spelling (`"PREREQUISITE_FAILED"`,
`"NOT_READY"`, `"disconnected"`), which an enum with C# member names cannot keep. Equality is by that
string, so `reason.Kind == ReasonKind.Off` and `switch` on constants work. `ClientStatus.LastError` is a
`string?` because `http_<status>` is open-ended; the three others (`timeout`, `bad_ruleset`, `network`) are
constants in `LastErrorCodes`. A `Reason` is `{ Kind, Code?, RuleIndex?, FlagKey? }`, the last two for
`RULE_MATCH` and `PREREQUISITE_FAILED`.

**Options with a non-zero default.** `Stream` is a plain `bool` initialised to `true` and each interval a
`TimeSpan` initialised to its default, in an `init`-only options class: "not given" is the initial value, so
a caller who does not set `Stream` gets `true` and one who sets `false` gets `false`; there is no
"unset" state to tell apart. A zero or negative `TimeSpan` falls back to the default (contract 2).

**Contexts.** A context is a `Context`, a `readonly struct` that holds a `JsonElement`. It converts
implicitly from `JsonElement`, so a raw JSON context passes through untouched (see "JSON"). Helpers build
the two shapes: `Context.ForKey(key, kind, attributes)`, `Context.Multi(...)` and `Context.FromJson(string)`.
A `default(Context)`, or a string that is not JSON, is an invalid context: `ERROR INVALID_CONTEXT`.

## 2. Concurrency

- **State.** One immutable `SyncState` record holds the ruleset (`Ruleset?`), its ETag, `lastSyncAt`, the
  stream state and `lastError`. It is read through a `volatile` field and replaced whole under a small
  `Lock` that only writers take and never hold across I/O, a callback or an `await`. A reader (an
  evaluation, `Status`) never takes it. So the ruleset and its ETag swap in one reference write, a reader
  sees the old state or the new one, `Status` is one snapshot, and the hook's `Etag` is the one of the
  ruleset the evaluation used because both come from the same `SyncState` read once.
- **Tasks.** `Create` starts two long-running tasks with `Task.Run` (thread pool threads are background
  threads, so a client never closed does not keep a process alive): the **sync loop** (first load, then
  the stream and poll loop of 7.3) and the **flush loop** (every `flushInterval`). Nothing else runs in the
  background. A request in flight is aborted by cancelling one `CancellationTokenSource` that `close` owns.
- **Close.** The first `Close` call takes the shutdown (a `Lazy`-style single task); every other call, in
  the same moment or later, awaits that same task. It runs in the contract's order: mark closing (state
  `closed`, `ready` completes `false` if unsettled), cancel the token (abandoned requests set no error and
  call no `onError`), await the sync loop, await the flush loop and any flush in progress, then the final
  flush with its own token that is not the cancelled one, bounded by `requestTimeout` per request, which
  does not wait out a usage hold.
- **Locks.** The writer lock above; the usage counters' lock (an add is a dictionary increment under it, a
  flush swaps the dictionary out under it and sends outside it); the flush mutex is a `SemaphoreSlim`
  serialising flushes (it is held across a usage request, but evaluation never takes it: `variation` only
  adds to the counters, whose lock is never held across I/O). So a hung ruleset or usage request cannot
  slow an evaluation.
- **Callbacks.** `afterEvaluation` and `onError` run with no SDK lock held, wrapped in a `try`/`catch`
  that sends a hook's exception to `onError` and ignores one from `onError` itself. A hook runs on the
  caller's thread, synchronously, before the call returns.
- **Close from a callback.** `Close` called from inside `onError` on a loop thread cannot wait for that
  loop. The loops record that they are the current context in an `AsyncLocal<bool>`; `Close` then does
  steps 1 and 2 and returns without joining, and the shutdown finishes on its own. The marker is an
  `AsyncLocal`, so it does not follow a continuation that a caller's own `TaskCompletionSource` runs inline
  from `onError` (it carries the caller's context): that `Close` waits for the loop it runs on. The README
  tells callers to complete such a source asynchronously; the watchdog tests found this.

## 3. HTTP and SSE

- **HTTP client:** one `HttpClient` over a `SocketsHttpHandler` from the standard library, with
  `Timeout = Timeout.InfiniteTimeSpan` (the SDK sets its own) and automatic decompression at its default of off (the SDK
  sends no `Accept-Encoding` of its own and asks for none). Redirects are not followed (`AllowAutoRedirect =
  false`): a `3xx` is `http_<status>`, which contract 7.1 allows.
- **`requestTimeout`, body included.** Each ruleset and events request gets a `CancellationTokenSource`
  with `CancelAfter(requestTimeout)`, linked to the close token, and that one token is passed to
  `SendAsync` and to the body read (`ReadAsByteArrayAsync(token)`), so the limit covers connecting, the
  headers and the whole body. `HttpClient.Timeout` is not used: it bounds the headers only when the
  response is read as a stream. When the token fires and `close` did not cancel it, the error is `timeout`.
- **The stream.** `GET /sdk/v1/stream` with `Accept: text/event-stream`, sent with `ResponseHeadersRead`
  under a `requestTimeout` token that bounds the wait for the headers only. After the headers, each read
  runs under its own token, `CancelAfter(watchdog)` (60 s; a seam on `create` shortens it for tests), so
  every read, a keep-alive included, restarts the 60 s, and a quiet stream is dropped as `timeout`.
- **SSE.** A hand-written parser (`Sse/SseParser.cs`), fed byte chunks. It decodes UTF-8 with a stateful
  `Decoder` so a code point split across reads is whole, and splits lines on LF, CRLF and a lone CR, keeping a
  trailing CR so a CRLF split between two chunks is one line end. It reads only `event` and `data`
  (several `data` lines joined with LF, a leading space after the colon removed, a comment line ignored),
  names the event `message` when none was given, drops an event with no `data` line, and discards an
  event still incomplete when the stream ends. No library is used.

## 4. Dependencies

- **Runtime: none.** The base class library has HTTP (`HttpClient`), JSON (`System.Text.Json`) and SHA-256
  (`System.Security.Cryptography.SHA256`). No NuGet package is referenced by `ToggleSlash.Sdk`.
- **Tests:** `xunit` 2.9.3, `xunit.runner.visualstudio` 3.1.5 and `Microsoft.NET.Test.Sdk` 18.11.0 (the
  versions the repository's own C# tests use) to run them; `Microsoft.Extensions.TimeProvider.Testing`
  (`FakeTimeProvider`), the standard fake-time facility, to test long timers without waiting.
- **Test service:** `Microsoft.NET.Sdk.Web` (Kestrel from the shared framework, no package) to be the HTTP
  server the protocol needs. It is not part of the SDK.
- **Lint:** the .NET SDK's own analyzers (`AnalysisLevel` latest, `AnalysisMode` recommended,
  `EnforceCodeStyleInBuild`, warnings as errors) and `dotnet format --verify-no-changes`. No package.

## 5. JSON

The parser is `System.Text.Json` (`JsonDocument`). The SDK reads a ruleset response with `JsonDocument.Parse`
and a context as a `JsonElement`. What the spec asks, and how it is kept:

- **Numbers** are read with `JsonElement.TryGetDouble` and a text fallback for values the reader rejects, in
  `Validation.TryNumber`: `1e999` is infinity, `9007199254740993` reads as `9007199254740992`, `-0.0` is
  negative zero and compares equal to `0`. A served value is returned as the `JsonElement` it was, so its
  text (`1e999`) survives into `JsonVariation`.
- **Lone surrogates.** A .NET `string` is UTF-16 code units, so it holds a lone surrogate as it is. The
  `JsonElement.GetString()` of `"\ud800"` replaces it with U+FFFD, so strings are read by
  `Evaluation/JsonStrings.cs`, which unescapes the raw JSON text by hand when it contains a lone surrogate.
  Hashing replaces a lone surrogate with U+FFFD only when it builds the bytes (spec, "Bucketing"); patterns
  read a string as code points by hand (`Pattern.cs`), not as `Rune`s, which would replace it earlier.
- **Duplicate keys** keep the last value (contract 7.1) in a ruleset; `JsonElement.TryGetProperty` returns
  the last of equal names. A leading byte order mark is skipped by hand before parsing, since
  `JsonDocument.Parse` rejects it.
- **The public API** takes a context as a `JsonElement` (inside `Context`), so a context with `1e999` or a
  lone surrogate reaches the evaluator exactly as it arrived. `Context.ForKey` builds from native values.
- A document nested deeper than `JsonDocument`'s default of 64 levels is an error; the SDK raises the limit
  to 256 for rulesets (`JsonDocumentOptions.MaxDepth`) and for contexts it receives from the test service.

## 6. Minimum runtime and toolchain

- **Minimum runtime:** net10.0, for the reason in the first section.
- **Lint and format:** `dotnet format --verify-no-changes` plus Roslyn analyzers as build errors, over the
  mainstream alternatives (StyleCop.Analyzers, CSharpier): they ship with the SDK, so nothing to install,
  and they need nothing beyond what `mise.toml` pins.
- **Test framework:** xunit 2.9.3, the one the repository's other C# tests already use and the mainstream
  one; xunit v3 offers nothing this SDK needs and would add a runner model not used in this repository.
- **Build tool:** `dotnet build` / `dotnet test` on the solution.
- **Integer accessor:** `IntVariation` returns `long`, the 64-bit signed type. A served number with a
  fractional part, or outside `long`, is `TYPE_MISMATCH`; `2.0` counts as `2`.
- **Race detector:** .NET has none, so `bin/test` names a stress test instead (see the checklist in
  `RUN.md`).

## 7. Package layout

```
sdks/csharp/
  mise.toml  global.json  .editorconfig  .gitignore  Directory.Build.props
  ToggleSlash.Sdk.slnx
  DESIGN.md  README.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/ToggleSlash.Sdk/              the library (namespace ToggleSlash.Sdk)
    Evaluation/                     the evaluator: no network, internal to the assembly
    Sse/SseParser.cs                the event stream parser
    ...                             the client, sync loop, usage reporter, options and result types
  tests/ToggleSlash.Sdk.Tests/      xunit: vectors and pins, then the client's native tests
  test-service/                     the conformance test service (a project in the solution)
```

The test service is a project of the solution, so `dotnet build ToggleSlash.Sdk.slnx` compiles it with the
tests and `bin/test` leaves its output in `artifacts/bin/ToggleSlash.Sdk.TestService/debug/` (`UseArtifactsOutput`, so `bin/` holds the scripts only). The NuGet package id and assembly are
`ToggleSlash.Sdk`; the convention has no host, so the repository path is not used.

The evaluator is `internal` and is reached by the tests through `InternalsVisibleTo`. Its port started from
`apps/server/ToggleSlash.Evaluation`, the repository's existing C# evaluator, which the prompt allows as a
reference; it is copied into the SDK, not referenced as a project, because an SDK has no dependency on the
server.

## Test seams

Options on `create` that are `internal` (visible to the tests through `InternalsVisibleTo`): an
`HttpMessageHandler`, a `TimeProvider` (the clock for `lastSyncAt`, the UTC day, every wait and every
`Retry-After` hold), a `Func<double>` jitter source, the stream watchdog length, and an evaluator wrapper
that can raise. None is a static or read from the environment.
