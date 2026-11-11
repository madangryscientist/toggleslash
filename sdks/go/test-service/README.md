# The Go SDK's test service

`main.go` wraps `toggleslash.NewClient` in the protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the SDK from
outside. From the repository root:

```sh
bun run conformance -- --service sdks/go/bin/test-service
```

`bin/test` builds it to `build/test-service`; `bin/test-service` builds it if needed and `exec`s it. It prints
`listening http://127.0.0.1:<port>` once it accepts connections.

It advertises one capability, `integer-accessor`: `IntVariation` and `IntVariationDetail` apply the integer
accessor rule of the contract.

| Protocol | Go SDK |
| --- | --- |
| `POST /clients` | `NewClient(url, sdkKey, opts...)`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` pass through as `WithStream`, `WithPollInterval`, `WithFlushInterval` and `WithRequestTimeout`; `OnError` always records |
| `ready` | `client.Ready()` |
| `evaluate`, accessor `bool` | `client.BoolVariationDetail(flagKey, ctx, fallback)` |
| `evaluate`, accessor `string` | `client.StringVariationDetail(flagKey, ctx, fallback)` |
| `evaluate`, accessor `number` | `client.FloatVariationDetail(flagKey, ctx, fallback)` |
| `evaluate`, accessor `int` | `client.IntVariationDetail(flagKey, ctx, fallback)` |
| `evaluate`, accessor `json` and `any` | `client.JSONVariationDetail(flagKey, ctx, fallback)`; a null fallback accepts any served kind |
| `status` | `client.Status()` as the four keys `etag`, `lastError`, `lastSyncAt` (epoch ms) and `stream`; `null` is none |
| `hook-events` | the events `WithAfterEvaluation` received, as the SDK built them |
| `errors` | what `WithOnError` received, each as its message |
| `close` | `client.Close()` |

A typed accessor whose fallback is not of its kind (for example `int` with `1.5`) answers 400: that is a mistake
in the scenario, not in the SDK. It catches nothing the SDK raises: a call that panics answers 500 with the
panic text and its stack, and the harness fails the scenario. The hook and `OnError` record first and then
panic when the scenario asks (`hookThrows`, `onErrorThrows`), which is how the SDK's own recovery is tested.
