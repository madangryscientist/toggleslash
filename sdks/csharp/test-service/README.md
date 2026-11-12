# The C# SDK's test service

`Program.cs` wraps `ToggleSlashClient` in the protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md),
so the conformance harness can drive the SDK from outside. It is a `Microsoft.NET.Sdk.Web` project (Kestrel from the
shared framework, no package) and one more project in `ToggleSlash.Sdk.slnx`; `bin/test` builds it last, and
`bin/test-service` runs the built output (and builds it only if it is missing):

```sh
bun run conformance -- --service sdks/csharp/bin/test-service
```

It listens on a port the OS picks on `127.0.0.1` and prints `listening http://127.0.0.1:<port>`. It advertises the
capability `integer-accessor`: `IntVariation` returns a `long`, and a fractional or out-of-int64 value is
`TYPE_MISMATCH`.

| Protocol | C# SDK |
| --- | --- |
| `POST /clients` | `ToggleSlashClient.Create(options)`; `pollIntervalMs`, `flushIntervalMs`, `requestTimeoutMs` and `stream` pass through as `TimeSpan?` and `bool?`; `onError` always records, and `onErrorThrows` makes it throw after recording |
| `ready` | `client.ReadyAsync()` |
| `evaluate`, accessor `bool` | `BoolVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `string` | `StringVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `number` | `FloatVariationDetail(flagKey, context, fallback)` (a `double`) |
| `evaluate`, accessor `int` | `IntVariationDetail(flagKey, context, fallback)` (a `long`, written back through a `double`, so a value past 2^53 is not exact on the wire, which the protocol allows) |
| `evaluate`, accessor `json` | `JsonVariationDetail(flagKey, context, fallback)` with the fallback as the `JsonValue` the SDK's own parser read |
| `evaluate`, accessor `any` | `JsonVariationDetail(...)`: a `null` fallback accepts any served kind, so it returns whatever the flag serves |
| `status` | `client.Status()`; `lastSyncAt` is epoch milliseconds or `null` |
| `hook-events` | the `EvaluationEvent`s `AfterEvaluation` received, as the SDK built them; `hookThrows` makes the hook throw after recording |
| `errors` | what `OnError` received, each as its message |
| `close` | `client.CloseAsync()` |

The context is handed to the SDK as the `JsonValue` its parser made from the request body, so a lone surrogate or
`1e999` reaches the evaluator as it was sent. The service catches nothing the SDK throws: a call that raises answers
500 with the exception text. It creates no `TaskCompletionSource` of its own; the SDK's every one uses `TaskCreationOptions.RunContinuationsAsynchronously`, because `Close()` from a continuation that ran inline would deadlock.
