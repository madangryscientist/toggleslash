# The Go SDK's test service

`main.go` wraps the SDK in the HTTP protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md) so
the conformance harness can drive it. It lives here, in the SDK's own module, and uses only the SDK's public
API. Start it with `bin/test-service`, which runs the binary `bin/test` built into `.build/` (and builds it if it
is missing); it prints `listening http://127.0.0.1:<port>` and serves any number of clients at once.

From the repository root:

```bash
bun run conformance -- --service sdks/go/bin/test-service
```

## Accessor mapping

| Protocol accessor | SDK method |
| --- | --- |
| `bool` | `BoolVariationDetail` |
| `string` | `StringVariationDetail` |
| `number` | `FloatVariationDetail` |
| `int` (capability `integer-accessor`) | `IntVariationDetail` (an `int64`) |
| `json` | `JSONVariationDetail` (a `Value`) |
| `any` | `AnyVariationDetail` (a `Value`) |

`json` and `any` are the same method here: Go has no type that is "any JSON value" except `Value`, and both
apply the contract's mismatch rule on kinds. A fallback that is not the accessor's kind (a string for `bool`)
is the harness's mistake, and answers 500.

## Mapping back

- The request body is read with the SDK's own JSON reader (`ParseValue`), so `1e999` and a lone surrogate escape
  reach the evaluator as they were sent; the context goes through `ContextFromValue`.
- A number that is not finite is written back as `null`, as the protocol says.
- `lastSyncAt` is `time.Time.UnixMilli()`, or `null` for the zero time.
- A hook event's `ETag` of `""` is `null` (nothing held), and `FlagVersion` is `null` when it is nil.
- `hook`, `hookThrows` and `onErrorThrows` install callbacks that record and, when asked, `panic`; the SDK
  recovers them.
