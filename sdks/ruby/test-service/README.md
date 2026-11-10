# The Ruby SDK's test service

`main.rb` wraps `ToggleSlash.create` in the protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the
SDK from outside. From the repository root:

```sh
bun run conformance -- --service sdks/ruby/bin/test-service
```

`bin/test-service` starts it with the toolchain `mise.toml` pins (under `ruby -w`) and prints
`listening http://127.0.0.1:<port>` once it serves. It is a `TCPServer` on a port the OS picks, one thread per
connection, so it is concurrent.

It advertises `integer-accessor`: Ruby has a separate `Integer` type, and `int_variation` bounds it to signed
64 bits.

| Protocol | Ruby SDK |
| --- | --- |
| `POST /clients` | `ToggleSlash.create(url:, sdk_key:, ...)`; `stream` passes through, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `poll_interval`, `flush_interval` and `request_timeout` in seconds, and `on_error` always records (and raises when `onErrorThrows` is set) |
| `ready` | `client.ready` |
| `evaluate`, accessor `bool` | `client.bool_variation_detail(flag_key, context, fallback)` |
| `evaluate`, accessor `string` | `client.string_variation_detail(...)` |
| `evaluate`, accessor `number` | `client.number_variation_detail(...)` |
| `evaluate`, accessor `int` | `client.int_variation_detail(...)` |
| `evaluate`, accessor `json` | `client.json_variation_detail(...)` |
| `evaluate`, accessor `any` | `client.variation_detail(...)`; a `nil` fallback accepts any served kind |
| `status` | `client.status` (`last_sync_at` as epoch milliseconds, `stream` and `last_error` as strings) |
| `hook-events` | the `ToggleSlash::EvaluationEvent`s that `hooks[:after_evaluation]` received, with the reason in its wire shape |
| `errors` | what `on_error` received, each as its message |
| `close` | `client.close` |

It catches nothing the SDK raises: a call that raises answers 500 with the class, message and backtrace.
