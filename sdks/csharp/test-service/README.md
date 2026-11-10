# The C# SDK's test service

`Program.cs` wraps the SDK in the protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so
the conformance harness can drive the SDK from outside. It is an ASP.NET Core (Kestrel) app on `127.0.0.1` and a
port the OS picks, with one request delegate and no routing library. It prints `listening http://127.0.0.1:<port>`
and flushes stdout, then serves concurrently (Kestrel's thread pool).

```bash
sdks/csharp/bin/test    # builds everything, the test service included, last
bun run conformance -- --service sdks/csharp/bin/test-service
```

**Where it lives.** `test-service/ToggleSlash.Sdk.TestService.csproj` is a project of
`ToggleSlash.Sdk.slnx`, next to the SDK, its tests and `tests/NeverClosed`. `bin/test` builds the solution
and then this project last, so `bin/test-service` runs the output `bin/test` left
(`artifacts/bin/ToggleSlash.Sdk.TestService/debug/ToggleSlash.Sdk.TestService.dll`) and builds only when it is
missing, because the harness waits 30 s for the `listening` line.

It advertises `integer-accessor`: C# has `long` next to `double`, so the `int` accessor is real.

| Protocol | C# SDK |
| --- | --- |
| `POST /clients` | `ToggleSlashClient.Create(new ClientOptions { ... })`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `Stream`, `PollInterval`, `FlushInterval` and `RequestTimeout` (`TimeSpan`); `OnError` always records the exception's message, and throws after recording when `onErrorThrows` is set |
| `ready` | `await client.ReadyAsync()` |
| `evaluate`, accessor `bool` | `client.BoolVariationDetail(key, context, fallback)` |
| `evaluate`, accessor `string` | `client.StringVariationDetail(key, context, fallback)` |
| `evaluate`, accessor `number` | `client.DoubleVariationDetail(key, context, fallback)` |
| `evaluate`, accessor `int` | `client.IntVariationDetail(key, context, fallback)`, the JSON fallback number taken as a `long` |
| `evaluate`, accessors `json` and `any` | `client.JsonVariationDetail(key, context, fallback)`, the fallback passed through as the JSON value it arrived as |
| `status` | `client.Status`, written as the four keys of the protocol (`LastSyncAt` as epoch milliseconds) |
| `hook-events` | the `EvaluationEvent`s `Hooks.AfterEvaluation` received (installed when `hook` or `hookThrows` is set; `hookThrows` throws after recording) |
| `errors` | what `ClientOptions.OnError` received, each as its message |
| `close` | `await client.CloseAsync()` |

The context goes in as raw JSON (`Context.FromElement`), valid or not: an invalid one is a case the SDK answers
with `ERROR INVALID_CONTEXT`. A call that fails or throws answers 500 with its text (the one `try` is in the request
delegate, only to send that 500), and the harness fails the scenario. A client stays answerable after `close`.
