# The Go SDK's test service

`main.go` wraps `toggleslash.Create` in the protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the
SDK from outside. From the repository root:

```sh
bun run conformance -- --service sdks/go/bin/test-service
```

`bin/test-service` runs `build/test-service`, which `bin/test` builds (it builds it first if it is missing)
with the toolchain `mise.toml` pins, and the program prints `listening http://127.0.0.1:<port>` once it
serves. It is `net/http` on a port the OS picks, one goroutine per connection, so it is concurrent.

It advertises `integer-accessor`: Go has `int64`, and `IntVariation` answers `TYPE_MISMATCH` for a served
number that is fractional or outside the signed 64 bit range.

| Protocol | Go SDK |
| --- | --- |
| `POST /clients` | `toggleslash.Create(url, sdkKey, opts...)`; `stream` becomes `WithStream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `WithPollInterval`, `WithFlushInterval` and `WithRequestTimeout` (a `time.Duration`), `WithOnError` always records (and panics when `onErrorThrows` is set), `WithAfterEvaluation` records when `hook` is set (and panics when `hookThrows` is set) |
| `ready` | `client.Ready()` |
| `evaluate`, accessor `bool` | `client.BoolVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `string` | `client.StringVariationDetail(...)` |
| `evaluate`, accessor `number` | `client.NumberVariationDetail(...)` |
| `evaluate`, accessor `int` | `client.IntVariationDetail(...)` |
| `evaluate`, accessor `json` | `client.JSONVariationDetail(...)` |
| `evaluate`, accessor `any` or none | `client.JSONVariationDetail(...)`; a null fallback accepts any served kind |
| `status` | `client.Status()` (`lastSyncAt` as epoch milliseconds, `etag`, `lastSyncAt` and `lastError` null when empty) |
| `hook-events` | the `EvaluationEvent`s the hook received, with the reason in its wire shape (`variationIndex` null when it is -1) |
| `errors` | what `WithOnError` received, each as its message |
| `close` | `client.Close()` |

It catches nothing the SDK panics with: a handler that panics answers 500 with the panic text, and the
harness fails the scenario.
