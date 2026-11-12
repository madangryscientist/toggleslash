# The Ruby SDK's conformance test service

`main.rb` wraps `Toggleslash.create` in the HTTP protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md) so the harness in `packages/sdk-conformance`
can drive the SDK. It uses the standard library only (`socket`, a few lines over `TCPServer`, one thread per
connection) and keeps a client answerable after `close`. Anything the SDK raises answers 500 with the error
text, and the harness fails the scenario.

Run it from the repository root:

```sh
bun run conformance -- --service sdks/ruby/bin/test-service
```

`bin/test-service` starts it under `mise` when `mise` is on the PATH, else under `ruby`; it prints
`listening http://127.0.0.1:<port>`. There is no build step: `bin/test` syntax-checks this file with the
sources and the tests, under `ruby -w` with warnings as errors.

## Capabilities

`integer-accessor`: the SDK has `int_variation` and `int_variation_detail`.

## Accessor mapping

| Harness accessor | SDK call |
| --- | --- |
| `bool`, `string`, `number`, `json`, `any` | `client.variation_detail(flag, context, fallback)`: the expected kind is read off the class of `fallback` (`true` or `false`, `Integer` or `Float`, `String`, `Array`, `Hash`; `nil` accepts any kind) |
| `int` | `client.int_variation_detail(flag, context, fallback)` (a whole `Float` fallback becomes an `Integer`) |

Request bodies are read with the SDK's own JSON reader (`Toggleslash::Json.parse`), so a number such as `1e999`
and a lone surrogate reach the SDK as the vectors give them. A non-finite number in an answer is sent as `null`
(JSON cannot hold it). `lastSyncAt` is sent as whole epoch milliseconds.
