# The Go SDK's test service

`main.go` wraps `toggleslash.New` in the protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md),
so the conformance harness can drive the SDK from outside:

```bash
bun run conformance -- --service sdks/go/bin/test-service
```

`bin/test-service` runs the binary that `bin/test` builds into `.build/`. The service listens on `127.0.0.1` on a
port the OS picks and prints `listening http://127.0.0.1:<port>` first.

It advertises one capability, `integer-accessor`: Go has an `int64` and `IntVariation`.

| Protocol | Go SDK |
| --- | --- |
| `POST /clients` | `toggleslash.New(url, sdkKey, opts...)`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `WithStream`, `WithPollInterval`, `WithFlushInterval` and `WithRequestTimeout`; `WithOnError` always records |
| `ready` | `Client.Ready()` |
| `evaluate`, accessor `bool` | `BoolVariationDetail` |
| `evaluate`, accessor `string` | `StringVariationDetail` |
| `evaluate`, accessor `number` | `Float64VariationDetail` |
| `evaluate`, accessor `int` | `IntVariationDetail`, with the answer written back through a double |
| `evaluate`, accessors `json` and `any` | `JSONVariationDetail`, with the fallback it is sent |
| `status` | `Client.Status()`; an empty ETag or `LastError` is `null`, `LastSyncAt` is epoch milliseconds or `null` |
| `hook-events` | the `Event`s `WithAfterEvaluation` received, with `FlagVersion` `NoVersion` written as `null` |
| `errors` | what `WithOnError` received, each as its `Error()` text |
| `close` | `Client.Close()` |

`hook: true` installs a recording hook; `hookThrows: true` also panics in it; `onErrorThrows: true` panics in the
recording `OnError` function. The service recovers nothing the SDK lets out of a call: a panic answers 500 with its
text, and the harness fails the scenario. A typed accessor sent a fallback of another kind answers 400.
