# The Go SDK's test service

`main.go` wraps `toggleslash.New` in the protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md),
so the conformance harness can drive the SDK from outside:

```bash
bun run conformance -- --service sdks/go/bin/test-service
```

It advertises one capability, `integer-accessor`: Go has an integer type and `IntVariation`.

| Protocol | Go SDK |
| --- | --- |
| `POST /clients` | `toggleslash.New(url, sdkKey, options...)`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` pass through as `WithStream`, `WithPollInterval`, `WithFlushInterval` and `WithRequestTimeout`, `WithOnError` always records |
| `ready` | `client.Ready()` |
| `evaluate`, accessor `bool` | `client.BoolVariationDetail(flagKey, ctx, fallback)` |
| `evaluate`, accessor `string` | `client.StringVariationDetail(flagKey, ctx, fallback)` |
| `evaluate`, accessor `number` | `client.FloatVariationDetail(flagKey, ctx, fallback)` |
| `evaluate`, accessor `int` | `client.IntVariationDetail(flagKey, ctx, fallback)`, with the fallback read as a whole number |
| `evaluate`, accessors `json` and `any` | `client.JSONVariationDetail(flagKey, ctx, fallback)`: the fallback is the JSON value as sent, null included |
| `status` | `client.Status()`, with `lastSyncAt` as epoch milliseconds and empty strings and the zero time as null |
| `hook-events` | the events `WithAfterEvaluation` received, as the SDK built them |
| `errors` | what `WithOnError` received, each as its message |
| `close` | `client.Close()` |

The context in an `evaluate` body goes to the SDK as it came, through `toggleslash.ContextFromValue`, so an
invalid one reaches the evaluator and gets `INVALID_CONTEXT`.

It catches nothing the SDK does wrong: a call that panics answers 500 with the error text.
