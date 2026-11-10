# ToggleSlash server SDK for Python

The Python server SDK of [ToggleSlash](../../README.md). It follows the language-neutral
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md), against a ruleset it keeps current from the server. It uses the
standard library only (Python 3.11 or newer) and is not published anywhere: use it from a checkout of this
repository.

## Quickstart

```sh
pip install ./sdks/python        # from the root of a checkout, in a virtual environment
```

```python
import toggleslash

client = toggleslash.create(
    "https://toggleslash.example.invalid",  # the server's base URL
    "sdk-key-from-the-dashboard",           # a server SDK key
)

if client.ready():  # waits for the first load attempt, bounded by request_timeout
    print("ruleset loaded")

context = {"key": "user-42", "plan": "pro"}
if client.variation("new-checkout", context, False):  # the fallback fixes the expected kind
    print("new checkout")

detail = client.variation_detail("banner-text", context, "welcome")
print(detail.value, detail.variation_index, detail.reason)

client.close()  # flushes usage counts and stops the background threads
```

`toggleslash.create` also works as a context manager target:
`with toggleslash.create(url, key) as client: ...` closes on exit.

## Mapping from the contract

The full table (every name, option and default, and what is a `Detail`, a `Status` and a `StreamState`) is
section 1 of [`DESIGN.md`](DESIGN.md). In short: `create`, `client.ready()`, `client.variation(...)`,
`client.variation_detail(...)`, `client.int_variation(...)` and `client.int_variation_detail(...)` for the
integer accessor, `client.status()`, `client.close()`; options `stream`, `poll_interval`, `flush_interval`,
`request_timeout` (seconds), `hooks` and `on_error`.

## Guarantees

- Nothing raises at a caller: a failure becomes your fallback with an `ERROR` reason, goes to `on_error`, or both.
- `variation`, `variation_detail` and `status` read memory only: they never wait on the network, a load or a flush.
- `ready()` settles after the first attempt, is bounded by `request_timeout`, and gives the same answer each time.
- The SDK keeps evaluating from the last good ruleset while the server is unreachable; only a valid ruleset replaces one.
- No evaluation context, attribute or SDK key appears in a request body or URL (the key is the `Authorization`
  header only), a log line, a status or an error the SDK builds.
- `close()` is safe to call twice and from many threads, and returns within about twice `request_timeout`.

## Check it

From the root of the repository:

```sh
sdks/python/bin/lint                                            # ruff check and ruff format --check
sdks/python/bin/test                                            # unittest, under python -X dev -W error, and every evaluation vector
bun run conformance -- --service sdks/python/bin/test-service   # the shared conformance harness
```

`mise.toml` pins Python and ruff; with `mise` installed the scripts use those versions. See
[`test-service/README.md`](test-service/README.md) for the conformance service, [`DESIGN.md`](DESIGN.md) for the
design decisions and [`RUN.md`](RUN.md) for the report of the run that wrote this SDK.
