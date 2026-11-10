# The Python SDK's test service

`main.py` wraps `toggleslash.create_client` in the protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the
SDK from outside. From the repository root:

```sh
bun run conformance -- --service sdks/python/bin/test-service
```

`bin/test-service` starts it with the toolchain `mise.toml` pins and prints `listening http://127.0.0.1:<port>`
once it serves. It is the standard library's `http.server` (threading), on a port the OS picks.

It advertises `integer-accessor`: Python has a separate integer type, and `int_variation` bounds it to
signed 64 bits.

| Protocol | Python SDK |
| --- | --- |
| `POST /clients` | `create_client(url, sdk_key, ...)`; `stream` passes through, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `poll_interval`, `flush_interval` and `request_timeout` in seconds, and `on_error` always records (and raises when `onErrorThrows` is set) |
| `ready` | `client.ready()` |
| `evaluate`, accessor `bool` | `client.bool_variation_detail(flag_key, context, fallback)` |
| `evaluate`, accessor `string` | `client.string_variation_detail(...)` |
| `evaluate`, accessor `number` | `client.float_variation_detail(...)` |
| `evaluate`, accessor `int` | `client.int_variation_detail(...)` |
| `evaluate`, accessor `json` | `client.json_variation_detail(...)` |
| `evaluate`, accessor `any` | `client.variation_detail(...)`; a `null` fallback accepts any served kind |
| `status` | `client.status()` (`last_sync_at` as epoch milliseconds, by integer arithmetic) |
| `hook-events` | the events `hooks.after_evaluation` received, as the SDK built them |
| `errors` | what `on_error` received, each as its message |
| `close` | `client.close()` |

It catches nothing the SDK raises: a call that raises answers 500 with the traceback.
