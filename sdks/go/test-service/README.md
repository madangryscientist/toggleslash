# The Go SDK's test service

`main.go` wraps the Go client in the protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so
the conformance harness can drive the SDK from outside:

```bash
bun run conformance -- --service sdks/go/bin/test-service
```

`bin/test` builds `test-service/test-service`; `bin/test-service` runs that binary and builds it only if it is
missing, so the harness never waits on a long build for its `listening` line.

It advertises `integer-accessor`: Go has `int64` and `IntVariation`.

| Protocol | Go SDK |
| --- | --- |
| `POST /clients` | `toggleslash.New(url, sdkKey, opts...)`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `WithStream`, `WithPollInterval`, `WithFlushInterval` and `WithRequestTimeout`; `onError` is always installed and records |
| `ready` | `client.Ready(ctx)` with the request's context |
| `evaluate`, accessor `bool` | `client.BoolVariationDetail` |
| `evaluate`, accessor `string` | `client.StringVariationDetail` |
| `evaluate`, accessor `number` | `client.FloatVariationDetail` |
| `evaluate`, accessor `int` | `client.IntVariationDetail`; the value is written from the `int64`, so every digit survives |
| `evaluate`, accessor `json` and `any` | `client.JSONVariationDetail`; the SDK has one generic JSON accessor |
| `status` | `client.Status()`; `lastSyncAt` is `UnixMilli`, `etag` and `lastError` are null when absent |
| `hook-events` | the `EvaluationEvent`s `WithAfterEvaluation` received |
| `errors` | what `WithOnError` received, each as its text |
| `close` | `client.Close()` |

A typed accessor given a fallback of another kind (or an `int` fallback with a fraction) answers 400, which
the protocol allows. A request that makes the SDK panic answers 500 with the text: nothing is absorbed here.
The service passes a context through as the JSON it received, an invalid one included, because answering
`INVALID_CONTEXT` is the SDK's job.
