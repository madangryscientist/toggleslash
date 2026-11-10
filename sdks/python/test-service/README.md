# The Python SDK's test service

`main.py` wraps the SDK in the protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the
conformance harness can drive the SDK from outside. It uses the standard library only (`http.server`'s
`ThreadingHTTPServer`, one thread per request), listens on `127.0.0.1` and a port the OS picks, prints
`listening http://127.0.0.1:<port>` and flushes stdout, and then serves.

```bash
sdks/python/bin/test    # the SDK's own tests, byte-compiling this directory too
bun run conformance -- --service sdks/python/bin/test-service
```

It advertises `integer-accessor`: Python has `int` next to `float`, so the `int` accessor is real.

| Protocol | Python SDK |
| --- | --- |
| `POST /clients` | `toggleslash.create(url=..., sdk_key=..., ...)`; `stream` is passed as is, and `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `poll_interval`, `flush_interval` and `request_timeout` in seconds; `on_error` always records the exception's text, then raises when `onErrorThrows` is set |
| `ready` | `client.ready()` |
| `evaluate`, accessor `bool` | `client.bool_variation_detail(key, context, fallback)` |
| `evaluate`, accessor `string` | `client.string_variation_detail(key, context, fallback)` |
| `evaluate`, accessor `number` | `client.number_variation_detail(key, context, fallback)` |
| `evaluate`, accessor `int` | `client.int_variation_detail(key, context, fallback)`, a whole-number fallback that JSON parsed as a float taken as an `int` |
| `evaluate`, accessor `json` | `client.json_variation_detail(key, context, fallback)` |
| `evaluate`, accessor `any` | `client.variation_detail(key, context, fallback)`, the generic method |
| `status` | `client.status()`, written as the four keys of the protocol (`last_sync_at` as epoch milliseconds, `stream` as its string) |
| `hook-events` | the `EvaluationEvent`s that `Hooks.after_evaluation` received, written with the protocol's camelCase keys (installed when `hook` or `hookThrows` is set; `hookThrows` raises after recording) |
| `errors` | what `on_error` received, each as its text |
| `close` | `client.close()` |

The context goes in as the JSON it arrived as, valid or not: an invalid one is a case the SDK answers with
`ERROR INVALID_CONTEXT`. A call that raises answers 500 with the traceback (the one `try` is in the request
handler, only to send that 500), and the harness fails the scenario. A client stays answerable after `close`.
