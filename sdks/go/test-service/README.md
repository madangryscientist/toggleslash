# The Go SDK's conformance test service

`main.go` wraps `toggleslash.New` in the HTTP protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md) so the harness in `packages/sdk-conformance`
can drive the SDK. It uses `net/http` for the server and the SDK's own `jsonvalue` package for every body it
reads and writes, so a number such as `1e999`, a lone surrogate or a duplicate key reaches the SDK as the vectors
give it. It serves each request on its own goroutine and keeps a client answerable after `close`. A request it
cannot read answers 400, a call that panics answers 500 with the panic text, and the harness fails the scenario.

Run it from the repository root:

```sh
bun run conformance -- --service sdks/go/bin/test-service
```

`bin/test-service` runs `.build/test-service`, which `bin/test` builds (the directory is ignored by git); if the
binary is missing it builds it first. It prints `listening http://127.0.0.1:<port>`.

## Capabilities

`integer-accessor`: the SDK has `IntVariation` and `IntVariationDetail`.

## Accessor mapping

| Harness accessor | SDK call | Fallback it must carry |
| --- | --- | --- |
| `bool` | `BoolVariationDetail(flag, ctx, bool)` | a boolean |
| `string` | `StringVariationDetail(flag, ctx, string)` | a string |
| `number` | `FloatVariationDetail(flag, ctx, float64)` | a number |
| `int` | `IntVariationDetail(flag, ctx, int64)` | an integer |
| `json` | `JSONVariationDetail(flag, ctx, jsonvalue.Value)` | anything; only its kind counts |
| `any` | `VariationDetail(flag, ctx, jsonvalue.Value)` | anything; `null` accepts any kind |

A fallback of the wrong JSON kind for a typed accessor is the service's own error (400), because a Go caller cannot
write it.

## Answers

- `value` is written with the SDK's JSON writer, so a number that is not finite is `null`; an `int` answer is
  written as an exact integer.
- `variationIndex` is `null` when the SDK reports `-1`.
- `reason` has `kind`, then `ruleIndex` for `RULE_MATCH`, `flagKey` and `code` when they are set.
- `status` is the SDK's `Status`, with `lastSyncAt` as whole epoch milliseconds (`null` for `-1`), and `etag` and
  `lastError` as `null` when empty.
- Hook events carry `flagKey`, `context`, `value`, `variationIndex`, `reason`, `etag` (`null` before a load) and
  `flagVersion` (`null` when the flag is not in the ruleset).
- `hookThrows` and `onErrorThrows` make the installed function panic after it records; the SDK recovers.
