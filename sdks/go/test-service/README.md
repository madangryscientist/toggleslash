# The Go SDK's conformance test service

`main.go` wraps `toggleslash.New` in the HTTP protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the harness in `packages/sdk-conformance` can
drive the SDK. It uses the standard library only (`net/http`, one goroutine per connection, so the harness's
parallel scenarios and a `ready` that waits do not queue behind each other) and keeps a client answerable after
`close`. Anything the SDK panics on answers 500 with the panic's text, and the harness fails the scenario.

`bin/test` builds it into `.build/test-service` as its last step; `bin/test-service` starts that binary and builds
it only when it is missing. Run it from the repository root:

```sh
bun run conformance -- --service sdks/go/bin/test-service
```

## Where it lives

`test-service/main.go` is a `main` package in this module, so `go build ./...` compiles it with the SDK and its
tests, and `go vet` and golangci-lint read it too. It reads its request bodies with the SDK's own JSON reader
(`internal/jsonx`), so a `1e999` and a lone surrogate reach the SDK exactly as the fixtures wrote them, and it
writes answers with the same package's writer, which sends a non-finite number as `null` (JSON cannot hold one).

## Capabilities

`integer-accessor`: the SDK has `Int64Variation` and `Int64VariationDetail`.

## Accessor mapping

| Harness accessor | SDK call |
| --- | --- |
| `bool` | `client.BoolVariationDetail(flag, context, fallback)` |
| `string` | `client.StringVariationDetail(flag, context, fallback)` |
| `number` | `client.Float64VariationDetail(flag, context, fallback)` (a whole number and a fraction are both numbers) |
| `int` | `client.Int64VariationDetail(flag, context, fallback)`: a whole `float64` fallback becomes an `int64` |
| `json`, `any` | `client.VariationDetail(flag, context, fallback)`: the expected kind is read off the fallback, and a `null` fallback accepts any kind |

A typed accessor given a fallback of another kind answers 500 (the protocol allows a 400 there; no scenario sends
one). `lastSyncAt`, a `time.Time` in the SDK, is sent as whole epoch milliseconds or null.
