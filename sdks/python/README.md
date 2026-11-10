# ToggleSlash server SDK for Python

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It
meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md), uses only the Python standard library (3.11 or
later) and is **not published**: there is no package index entry to install from.

## Quickstart

Install from a checkout of this repository:

```sh
pip install ./sdks/python
```

or put `sdks/python/src` on `PYTHONPATH`. Then:

```python
import toggleslash

client = toggleslash.create_client("https://flags.example.com", "sdk-key-from-your-environment")
try:
    client.ready()  # optional: waits for the first load, never longer than request_timeout
    context = {"key": "user-42", "plan": "pro"}
    if client.bool_variation("new-checkout", context, False):
        ...
finally:
    client.close()  # sends the last usage counts and stops the two background threads
```

`create_client` returns at once and never raises. `variation` and its typed siblings never wait for the
network: before the first load they answer your fallback with the reason `ERROR` / `NOT_READY`. A `Client` is
also a context manager (`with toggleslash.create_client(...) as client:`).

Read the key from the environment, never from source.

## The API

Every name in the contract and its Python spelling is in the table in
[`DESIGN.md`](DESIGN.md#the-mapping-table), along with the design choices behind it.

## Guarantees

The contract's section 5, as this SDK keeps it:

1. It never raises at a caller, whatever the server, the network, a context, a hook or `on_error` does.
2. `variation`, `variation_detail` and `status` never do I/O and never wait.
3. `ready` settles after the first attempt and gives the same answer every time.
4. While the server is unreachable it keeps evaluating from the last good ruleset.
5. Only something with the shape of a ruleset replaces a ruleset.

It also sends no context, context key or attribute anywhere: usage counts carry a flag, a variation index and
a day.

## Running the checks

The toolchain is pinned in [`mise.toml`](mise.toml) (`mise install` from this directory). The `bin` scripts
find it themselves.

```sh
sdks/python/bin/lint      # ruff check and ruff format --check
sdks/python/bin/test      # byte-compile, then every unit test and every evaluation vector, with warnings as errors
bun run conformance -- --service sdks/python/bin/test-service   # from the repository root: the conformance harness
```

The test service is described in [`test-service/README.md`](test-service/README.md). A report of the run that
built this SDK is in [`RUN.md`](RUN.md).
