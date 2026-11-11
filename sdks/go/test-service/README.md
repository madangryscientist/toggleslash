# The Go SDK's conformance test service

`main.go` wraps `toggleslash.New` in the HTTP protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md) so the harness in `packages/sdk-conformance`
can drive the SDK. It is a `package main` in the same module as the SDK, uses only the standard library
(`net/http`), serves requests on goroutines, and keeps a client answerable after `close`. A panic in a
handler answers 500 and the harness fails the scenario.

It is compiled to `sdks/go/.build/test-service` with `go build -o .build/test-service ./test-service`;
`bin/test` builds it last, and `bin/test-service` builds it only when that binary is missing, then runs it.
Run it from the repository root:

```sh
bun run conformance -- --service sdks/go/bin/test-service
```

It listens on `127.0.0.1:0` and prints `listening http://127.0.0.1:<port>`.

## Capabilities

`integer-accessor`: the SDK has `Int64Variation` and `Int64VariationDetail`.

## Accessor mapping

| Harness accessor | SDK call |
| --- | --- |
| `bool` | `client.BoolVariationDetail(flag, context, fallback)` |
| `string` | `client.StringVariationDetail(flag, context, fallback)` |
| `number` | `client.Float64VariationDetail(flag, context, fallback)` |
| `int` | `client.Int64VariationDetail(flag, context, fallback)` |
| `json`, `any` | `client.VariationDetail(flag, context, fallback)`: the expected kind is read off the fallback (`null` accepts any kind) |

A typed accessor given a fallback of another kind answers 400. The request context goes through
`toggleslash.ContextFromValue`, and request bodies are read with the SDK's own JSON reader
(`jsonvalue.Parse`), so a number such as `1e999` and a lone surrogate reach the SDK as the vectors give them.
A non-finite number in an answer is sent as `null` (JSON cannot hold it). `lastSyncAt` is sent as whole epoch
milliseconds, and an empty `etag`, `lastError` or `flagVersion` as `null`.
