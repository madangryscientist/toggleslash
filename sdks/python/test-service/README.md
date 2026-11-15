# The Python SDK's conformance test service

`main.py` wraps `toggleslash.create` in the HTTP protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md) so the harness in `packages/sdk-conformance`
can drive the SDK. It uses the standard library only (`http.server.ThreadingHTTPServer`, one thread per
connection, so the harness's parallel scenarios and a `ready` that waits do not queue behind each other) and
keeps a client answerable after `close`. Anything the SDK raises answers 500 with the error text, and the
harness fails the scenario.

Run it from the repository root:

```sh
bun run conformance -- --service sdks/python/bin/test-service
```

`bin/test-service` starts it with `-X dev -W error` on the project-local venv `bin/venv` makes (the mise Python,
plus the `signing` extra for `POST /sign`); it prints `listening http://127.0.0.1:<port>`. There is no build
step: `bin/test` compiles this file with the sources and the tests, under `-W error`, so a syntax error here fails
there. It puts `../src` on the import path itself.

## Capabilities

`integer-accessor`: the SDK has `int_variation` and `int_variation_detail`.

`sign-context`: `POST /sign` calls `toggleslash.sign_context(privateKeyPem, context)`. It is the one route that
catches what the SDK raises: a `ValueError` (a key that is not P-256, an invalid context) or an `ImportError`
answers `400 { "error": "<text>" }` (contract 14).

## Accessor mapping

| Harness accessor | SDK call |
| --- | --- |
| `bool`, `string`, `number`, `json`, `any` | `client.variation_detail(flag, context, fallback)`: the expected kind is read off the type of `fallback` (`bool`, `int` or `float`, `str`, `list`, `dict`; `None` accepts any kind) |
| `int` | `client.int_variation_detail(flag, context, fallback)` (a whole `float` fallback becomes an `int`) |

Request bodies are read with the SDK's own JSON reader (`toggleslash._json.loads`), so a number such as `1e999`
and a lone surrogate reach the SDK as the vectors give them. A non-finite number in an answer is sent as `null`
(JSON cannot hold it). `lastSyncAt`, a `datetime` in the SDK, is sent as whole epoch milliseconds.
