# The Go SDK's test service

`test-service/main.go` wraps `toggleslash.Client` in the protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the SDK
from outside:

```bash
bun run conformance -- --service "sdks/go/bin/test-service"
```

It is `package main` in `sdks/go/test-service/`, inside the module, so `go build ./...` compiles it with
everything else. `sdks/go/bin/test` builds the binary as its last step, into `build/testservice` (ignored by
`sdks/go/.gitignore`), and `sdks/go/bin/test-service` runs that binary and builds it only when it is missing.
The service listens on `127.0.0.1`, on a port the OS picks, and prints `listening http://127.0.0.1:<port>` on
a line of its own.

It advertises `integer-accessor`: Go's `int64` is an integer type distinct from `float64`, and
`IntVariation` is the integer accessor.

| Protocol | Go SDK |
| --- | --- |
| `POST /clients` | `toggleslash.New(toggleslash.Options{...})`; `url`, `sdkKey`, `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` pass through, `onError` always records, `hook` and `hookThrows` install `Hooks.AfterEvaluation` (which records, then panics for `hookThrows`), and `onErrorThrows` panics after recording |
| `ready` | `(*Client).Ready(context.Background())` |
| `evaluate`, `bool` | `(*Client).BoolVariationDetail(flagKey, context, fallback)` with the fallback as a `bool` |
| `evaluate`, `string` | `StringVariationDetail` with the fallback as a `string` |
| `evaluate`, `number` | `FloatVariationDetail` with the fallback as a `float64` |
| `evaluate`, `int` | `IntVariationDetail` with the fallback as an `int64`; the answer is written back through a `float64`, as the protocol page allows |
| `evaluate`, `json` | `JSONVariationDetail` with the fallback as a `jsonval.Value`, null included |
| `evaluate`, `any` | `JSONVariationDetail`, the closest Go has to a generic method: `jsonval.Value` holds any fallback kind, null included |
| `status` | `(*Client).Status()`: `etag`, `lastSyncAt` as whole epoch milliseconds or `null`, `stream`, `lastError` |
| `hook-events` | the events `Hooks.AfterEvaluation` received, as `{ flagKey, context, value, variationIndex, reason, etag, flagVersion }` |
| `errors` | what `onError` received, each as its message |
| `close` | `(*Client).Close()` |

A reason is `{ kind }`, plus `ruleIndex` for `RULE_MATCH`, `flagKey` when a prerequisite failed and `code`
for `ERROR`, which is the shape the vectors expect. An answer that is not a finite number is written as JSON
`null`, as `JSON.stringify` writes it.

It absorbs nothing the SDK raises: a handler that panics answers 500 with the panic's text, and the harness
fails the scenario.
