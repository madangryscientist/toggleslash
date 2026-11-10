# ToggleSlash server SDK for Python

The Python server SDK for ToggleSlash. It follows [`docs/sdk-contract.md`](../../docs/sdk-contract.md) and
evaluates flags locally to [`docs/evaluation.md`](../../docs/evaluation.md). It has no runtime dependency
(standard library only) and is not published to any registry.

## Quickstart

Install from a checkout of this repository:

```sh
pip install ./sdks/python
```

```python
from toggleslash import create_client

client = create_client(url="https://flags.example.com", sdk_key="sdk-...")
client.ready()  # optional: waits for the first load; evaluation never needs it

context = {"key": "user-42", "attributes": {"plan": "pro"}}
if client.bool_variation("new-checkout", context, False):
    ...

detail = client.string_variation_detail("banner", context, "none")
print(detail.value, detail.variation_index, detail.reason)

client.close()  # sends the last usage counts; or use `with create_client(...) as client:`
```

Python is `>=3.11`. `create_client` returns at once and loads on a background thread.

## Mapping to the contract

The full table, with every option and its default, is in [`DESIGN.md`](DESIGN.md#the-mapping-table). In short:
`create_client(...)`, `ready()`, `variation(...)` and the typed `bool_variation`, `string_variation`,
`float_variation`, `int_variation`, `json_variation` (each with a `_detail` form), `status()`, `close()`.
Options are keyword-only: `url`, `sdk_key`, `stream`, `poll_interval`, `flush_interval`, `request_timeout`
(seconds, or a `timedelta`), `hooks=Hooks(after_evaluation=...)` and `on_error`.

## Guarantees

- It never raises at a caller: a bad context, a flag it cannot evaluate, a hook that raises and an
  `on_error` that raises are all absorbed.
- `variation` and `status` do no I/O and never wait on the network.
- It keeps the last good ruleset when the server is down, slow or sends something that is not a ruleset.
- Usage counts are batched and sent by `flush_interval` and by `close`; they hold flag keys, variation
  indices and days, never a context.
- Nothing about the end user leaves the process: no context, key or attribute is in a request, a log or an
  error, and there is no telemetry.
- `close` is idempotent and safe from any thread; after it the client still answers from the last ruleset.
- A client you never close does not keep the process alive (its two threads are daemons).

## Checks

From the repository root (`bun install` once), with [mise](https://mise.jdx.dev) providing the pinned Python
and ruff, or any Python 3.11+ and ruff on `PATH`:

```sh
sdks/python/bin/lint   # ruff check and ruff format --check
sdks/python/bin/test   # byte-compile, then unittest under -X dev -W error
bun run conformance -- --service sdks/python/bin/test-service
```

The conformance harness plays the application and the ToggleSlash server around the SDK's
[test service](test-service/README.md). [`RUN.md`](RUN.md) is the report of the run that wrote this SDK.
