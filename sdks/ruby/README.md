# ToggleSlash server SDK for Ruby

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It
meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md), has no runtime dependency beyond the Ruby standard
library (3.3 or later) and is **not published**: there is no package index entry to install from.

## Quickstart

Install from a checkout of this repository, by putting `sdks/ruby/lib` on the load path (`ruby -I sdks/ruby/lib`,
or `$LOAD_PATH.unshift`), or with a Gemfile line that points at the directory:

```ruby
gem 'toggleslash', path: 'sdks/ruby'
```

Then:

```ruby
require 'toggleslash'

client = ToggleSlash.create(url: 'https://flags.example.com', sdk_key: ENV.fetch('TOGGLESLASH_SDK_KEY'))
begin
  client.ready # optional: waits for the first load, never longer than request_timeout
  context = { 'key' => 'user-42', 'plan' => 'pro' }
  if client.bool_variation('new-checkout', context, false)
    # ...
  end
ensure
  client.close # sends the last usage counts and stops the two background threads
end
```

`create` returns at once and never raises. `variation` and its typed siblings never wait for the network:
before the first load they answer your fallback with the reason `:ERROR` / `:NOT_READY`.

Read the key from the environment, never from source.

## The API

Every name in the contract and its Ruby spelling is in the table in
[`DESIGN.md`](DESIGN.md#mapping-table-contract-section-2), along with the design choices behind it. In short:

| Contract | Ruby |
| --- | --- |
| `create` | `ToggleSlash.create(url:, sdk_key:, stream: true, poll_interval: 30, flush_interval: 30, request_timeout: 10, hooks: nil, on_error: nil)` |
| `ready` | `client.ready` returns `true` or `false` |
| `variation`, `variationDetail` | `client.variation(flag_key, context, fallback)`, `client.variation_detail(...)` |
| typed accessors | `bool_`, `string_`, `number_`, `int_`, `json_` + `variation` and `variation_detail` |
| `status` | `client.status` returns `ToggleSlash::Status` (`etag`, `last_sync_at`, `stream`, `last_error`) |
| `close` | `client.close` |
| `hooks.afterEvaluation` | `hooks: { after_evaluation: ->(event) { ... } }` |
| `onError` | `on_error: ->(error) { ... }` |

Durations are `Numeric` seconds. Reason kinds, error codes, stream states and `last_error` codes are Symbols
with the contract's spelling (`:RULE_MATCH`, `:NOT_READY`, `:connected`, `:http_503`).

## Guarantees

The contract's section 5, as this SDK keeps it:

1. It never raises at a caller, whatever the server, the network, a context, a hook or `on_error` does.
2. `variation`, `variation_detail` and `status` never do I/O and never wait.
3. `ready` settles after the first attempt and gives the same answer every time.
4. While the server is unreachable it keeps evaluating from the last good ruleset.
5. Only something with the shape of a ruleset replaces a ruleset.

It also sends no context, context key or attribute anywhere: usage counts carry a flag, a variation index and
a day. A client that is never closed does not keep the process from exiting (its two threads do not outlive
the main thread).

## Running the checks

The toolchain is pinned in [`mise.toml`](mise.toml) (`mise install` from this directory). The `bin` scripts
find it themselves.

```sh
sdks/ruby/bin/lint      # ruby -wc on every file, then RuboCop
sdks/ruby/bin/test      # every unit test and every evaluation vector, with Ruby warnings as errors
bun run conformance -- --service sdks/ruby/bin/test-service   # from the repository root: the conformance harness
```

The test service is described in [`test-service/README.md`](test-service/README.md). A report of the run that
built this SDK is in [`RUN.md`](RUN.md).
