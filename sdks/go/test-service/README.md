# The Go SDK's test service

`main.go` wraps the SDK in the protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md),
so the conformance harness can drive the SDK from outside:

```bash
sdks/go/bin/test    # builds .build/test-service as its last step
bun run conformance -- --service "sdks/go/bin/test-service"
```

It advertises `integer-accessor`: Go has `int64` next to `float64`, so the `int` accessor is real.

| Protocol | Go SDK |
| --- | --- |
| `POST /clients` | `toggleslash.New(url, sdkKey, options...)`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `WithStream`, `WithPollInterval`, `WithFlushInterval` and `WithRequestTimeout`; `onError` always records, and panics after recording when `onErrorThrows` is set |
| `ready` | `client.Ready()` |
| `evaluate`, accessor `bool` | `client.BoolVariationDetail(key, ctx, fallback)` |
| `evaluate`, accessor `string` | `client.StringVariationDetail(key, ctx, fallback)` |
| `evaluate`, accessor `number` | `client.Float64VariationDetail(key, ctx, fallback)` |
| `evaluate`, accessor `int` | `client.IntVariationDetail(key, ctx, fallback)` |
| `evaluate`, accessors `json` and `any` | `client.JSONVariationDetail(key, ctx, fallback)`, the fallback passed as a JSON value |
| `status` | `client.Status()`, written as the four keys of the protocol |
| `hook-events` | the events `WithAfterEvaluation` received (installed when `hook` or `hookThrows` is set; `hookThrows` panics after recording) |
| `errors` | what `WithOnError` received, each as its message |
| `close` | `client.Close()` |

The context goes in as raw JSON (`ContextFromValue`), valid or not: an invalid one is a case the SDK
answers with `ERROR INVALID_CONTEXT`. A call that fails or panics answers 500 with its text, and the harness
fails the scenario. The service serves concurrently and keeps a client answerable after `close`.

`bin/test-service` starts the built binary and builds it only if it is missing, because the harness waits 30 s
for the `listening` line.
