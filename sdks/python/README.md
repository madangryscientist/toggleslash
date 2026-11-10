# ToggleSlash server SDK for Python

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It meets
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), has no dependency beyond the Python standard library (3.11 or
later) and is **not published**: there is no package index entry to install from.

## Quickstart

Install from a checkout of this repository, by putting `sdks/python/src` on the import path
(`PYTHONPATH=sdks/python/src`, or `sys.path.insert`), or with `pip install sdks/python` into a virtual environment.

```python
import os
import toggleslash

client = toggleslash.create(url="https://flags.example.com", sdk_key=os.environ["TOGGLESLASH_SDK_KEY"])
try:
    client.ready()  # optional: waits for the first load, never longer than request_timeout (plus a second)
    context = {"key": "user-42", "plan": "pro"}
    if client.bool_variation("new-checkout", context, False):
        ...
finally:
    client.close()  # sends the last usage counts and stops the two background threads
```

`Client` is also a context manager (`with toggleslash.create(...) as client:` closes it). `create` returns at once.
`variation` and its typed siblings never wait for the network: before the first load they answer your fallback with the
reason `NOT_READY`. Read the key from the environment, never from source.

## The API

Every name in the contract and its Python spelling is in the table in
[`DESIGN.md`](DESIGN.md#1-the-mapping-table), along with the design choices behind it. In short:

| Contract | Python |
| --- | --- |
| `create` | `toggleslash.create(url=, sdk_key=, stream=True, poll_interval=30, flush_interval=30, request_timeout=10, hooks=None, on_error=None)` |
| `ready` | `client.ready()` returns `True` or `False` |
| `variation`, `variationDetail` | `client.variation(flag_key, context, fallback)`, `client.variation_detail(...)` |
| typed accessors | `bool_`, `string_`, `number_`, `int_`, `json_` + `variation` and `variation_detail` |
| `status` | `client.status()` returns a `Status` (`etag`, `last_sync_at`, `stream`, `last_error`) |
| `close` | `client.close()` |
| `hooks.afterEvaluation` | `hooks=Hooks(after_evaluation=callable)` |
| `onError` | `on_error=callable` |

Durations are seconds (`int`, `float`) or a `datetime.timedelta`. Reason kinds, error codes and stream states are
`StrEnum`s with the contract's spelling (`ReasonKind.RULE_MATCH`, `ErrorCode.NOT_READY`, `StreamState.CONNECTED`).

## Guarantees

The contract's section 5, as this SDK keeps it:

- It never raises at a caller: `create`, `ready`, `variation`, `variation_detail`, `status` and `close` turn a
  failure into your fallback with an `ERROR` reason, or into an `on_error` call, or both.
- `variation` and `variation_detail` never do I/O and never wait: they read memory and run the evaluator. `status`
  is the same.
- `ready` settles after the first attempt and is bounded by `request_timeout`; its answer is then fixed.
- While the server is unreachable the SDK keeps evaluating from the last good ruleset.
- Only a ruleset replaces a ruleset: an answer that is not one is a `bad_ruleset` error and replaces nothing.
- The two background threads are daemon threads, so a client you forget to close never keeps the process alive.

No end-user data leaves the process: a request carries the key, the ruleset tag and the usage counts, never a
context.

## Develop

Python 3.15.2 and ruff 0.17.4 are pinned in [`mise.toml`](mise.toml); the scripts use `mise exec` when `mise` is
on the PATH and the tools on the PATH otherwise.

```bash
sdks/python/bin/lint    # ruff check and ruff format --check, no warning allowed
sdks/python/bin/test    # byte-compiles, then every unit test and the shared evaluation vectors, under -X dev -W error
bun run conformance -- --service sdks/python/bin/test-service    # from the repository root: the SDK harness
```

[`test-service/README.md`](test-service/README.md) says how the test service maps onto the SDK.
