# ToggleSlash server SDK for Ruby

The Ruby server SDK of [ToggleSlash](../../README.md). It follows the language-neutral
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md), against a ruleset it keeps current from the server. It has no
runtime dependency (Ruby 3.3 or newer; the standard library only) and is not published anywhere: use it from a
checkout of this repository.

## Quickstart

In a `Gemfile`, from a checkout of this repository (no tag, no registry):

```ruby
gem "toggleslash", path: "path/to/checkout/sdks/ruby"
```

or add `sdks/ruby/lib` to the load path.

```ruby
require "toggleslash"

client = Toggleslash.create(
  url: "https://toggleslash.example.invalid", # the server's base URL
  sdk_key: "sdk-key-from-the-dashboard"       # a server SDK key
)

puts "ruleset loaded" if client.ready # waits for the first load attempt, bounded by request_timeout

context = { "key" => "user-42", "plan" => "pro" }
puts "new checkout" if client.variation("new-checkout", context, false) # the fallback fixes the expected kind

detail = client.variation_detail("banner-text", context, "welcome")
puts [detail.value, detail.variation_index, detail.reason.kind].inspect

client.close # flushes usage counts and stops the background threads
```

## Mapping from the contract

The full table (every name, option and default, and what is a `Detail`, a `Status` and a `StreamState`) is
section 1 of [`DESIGN.md`](DESIGN.md). In short: `Toggleslash.create`, `client.ready`, `client.variation(...)`,
`client.variation_detail(...)`, `client.int_variation(...)` and `client.int_variation_detail(...)` for the
integer accessor, `client.status`, `client.close`; keyword options `stream`, `poll_interval`, `flush_interval`,
`request_timeout` (seconds), `hooks` (`Toggleslash::Hooks.new(after_evaluation: callable)`) and `on_error`.

## Guarantees

- Nothing raises at a caller: a failure becomes your fallback with an `ERROR` reason, goes to `on_error`, or both.
- `variation`, `variation_detail` and `status` read memory only: they never wait on the network, a load or a flush.
- `ready` settles after the first attempt, is bounded by `request_timeout`, and gives the same answer each time.
- The SDK keeps evaluating from the last good ruleset while the server is unreachable; only a valid ruleset replaces one.
- No evaluation context, attribute or SDK key appears in a request body or URL (the key is the `Authorization`
  header only), a log line, a status or an error the SDK builds.
- `close` is safe to call twice and from many threads, and returns within about twice `request_timeout`.

## Check it

Install the toolchain once, then run from the root of the repository:

```sh
(cd sdks/ruby && mise trust && mise install)
sdks/ruby/bin/lint                                            # RuboCop, then a syntax check under ruby -w
sdks/ruby/bin/test                                            # Minitest under ruby -w, and every evaluation vector
bun run conformance -- --service sdks/ruby/bin/test-service   # the shared conformance harness
```

`mise.toml` pins Ruby and RuboCop; with `mise` installed the scripts use those versions. See
[`test-service/README.md`](test-service/README.md) for the conformance service, [`DESIGN.md`](DESIGN.md) for the
design decisions and [`RUN.md`](RUN.md) for the report of the run that wrote this SDK.
