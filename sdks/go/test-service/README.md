# The Go SDK's test service

`main.go` wraps `toggleslash.New` in the protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md),
so the conformance harness can drive the SDK from outside:

```bash
bun run conformance -- --service sdks/go/bin/test-service
```

It advertises one capability, `integer-accessor`: Go has `int64` as well as `float64`.

| Protocol | Go SDK |
| --- | --- |
| `POST /clients` | `toggleslash.New(url, sdkKey, opts...)`; `pollIntervalMs`, `flushIntervalMs`, `requestTimeoutMs` and `stream` pass through `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout` and `WithStream`; `WithOnError` and `WithAfterEvaluation` always record |
| `ready` | `client.Ready()` |
| `evaluate`, accessor `any` or `json` | `client.VariationDetail(flagKey, context, fallback)`: the generic method, with the fallback's kind doing the work (a `null` fallback accepts any kind) |
| `evaluate`, accessor `bool` | `client.BoolVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `string` | `client.StringVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `number` | `client.NumberVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `int` | `client.IntVariationDetail(flagKey, context, fallback)`, which answers `TYPE_MISMATCH` for a fractional value or one outside int64 |
| `status` | `client.Status()`; `lastSyncAt` is whole epoch milliseconds or `null` |
| `hook-events` | the `EvaluationEvent`s `WithAfterEvaluation` received, as the SDK built them |
| `errors` | what `WithOnError` received, each as its message |
| `close` | `client.Close()` |

A typed accessor whose fallback is not of its kind (a `string` accessor with a number fallback) answers 400,
as the protocol allows: Go's typed accessors take a typed fallback, so there is no such call to make.

It catches nothing the SDK raises: a call that panics answers 500 with the error text, and so does any
error the SDK returns. A client stays answerable after `close`. The service listens on `127.0.0.1:0` and
prints `listening http://127.0.0.1:<port>` first. Non-finite numbers are written as `null`.
