# The Python test service

`main.py` wraps the SDK in the HTTP server of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md),
so `packages/sdk-conformance` can drive it. Run the whole check from the repository root:

```bash
bun run conformance -- --service sdks/python/bin/test-service
```

`bin/test-service` starts it from `sdks/python` (never start it with `src/toggleslash` as the working
directory: that package has a `types.py` and an `http.py` that would shadow the standard library's). It
listens on `127.0.0.1` on a port the OS picks, prints `listening http://127.0.0.1:<port>`, and serves each
request on its own thread.

It advertises `integer-accessor`: Python has an `int` and the SDK has `int_variation`.

## Accessor mapping

| `accessor` | SDK method | Kind it accepts |
| --- | --- | --- |
| `bool` | `bool_variation_detail` | boolean |
| `string` | `string_variation_detail` | string |
| `number` | `float_variation_detail` | number (the value comes back as a `float`) |
| `int` | `int_variation_detail` | a number with no fractional part inside the signed 64-bit range |
| `json` | `json_variation_detail` | array or object (the kind of a null fallback is not checked) |
| `any` | `variation_detail` | the kind of the fallback; a null fallback accepts any |

Every answer is `{ value, variationIndex, reason }`, with `Reason.to_json()` as `reason`.

## Options and switches

`url`, `sdkKey` and `stream` are passed through. `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs`
are divided by 1000 and passed as `poll_interval`, `flush_interval` and `request_timeout` (seconds).
`hook` and `hookThrows` install a `Hooks(after_evaluation=...)` that records the event as
`{ flagKey, context, value, variationIndex, reason, etag, flagVersion }`; `hookThrows` then raises.
`onErrorThrows` makes the `on_error` the service installs (which always records `str(error)`) raise after
recording. `status` is `Status` with `last_sync_at` turned into whole epoch milliseconds.

## Errors

The service catches nothing the SDK raises: an exception answers `500` with the traceback, and the
harness fails the scenario. An unknown client or route answers `404`.
