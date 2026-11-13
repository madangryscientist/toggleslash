# ToggleSlash Go server SDK

A server SDK for ToggleSlash, a source-available feature flag service, in Go. It loads an
environment's ruleset, follows it over a stream (or polls), evaluates flags in this process, and
reports usage counts. It is a module in this repository and is not a published package: there is no
tag and no registry entry.

## Quickstart

The module has no release tag, so a consumer points at a checkout of this repository: clone it, then
add a `replace` to your own module (or a `go.work` entry) beside the `require`.

```sh
git clone https://github.com/madangryscientist/toggleslash
```

In your module:

```sh
go mod edit -require=github.com/madangryscientist/toggleslash/sdks/go@v0.0.0
go mod edit -replace=github.com/madangryscientist/toggleslash/sdks/go=/path/to/toggleslash/sdks/go
```

Then create a client, read a flag, and close it:

```go
package main

import (
	"fmt"
	"log"

	"github.com/madangryscientist/toggleslash/sdks/go"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

func main() {
	client, err := toggleslash.New(toggleslash.Options{
		URL:    "http://127.0.0.1:8080",
		SDKKey: "sdk-server-key",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	context := jsonval.Object{"key": jsonval.String("user-42")}
	fmt.Println(client.BoolVariation("new-checkout", context, false))
}
```

`New` waits for nothing: it starts the sync loop, the usage flush and the error delivery, and
returns. `Ready(ctx)` reports whether the first load settled. `Close` is idempotent and sends the
counts it still holds.

## The API

| Name | Go |
| --- | --- |
| `create` | `New(opts Options) (*Client, error)` |
| `ready` | `(*Client) Ready(ctx context.Context) bool` |
| `variation` | `BoolVariation`, `StringVariation`, `IntVariation`, `FloatVariation`, `JSONVariation` (fallback fixes the kind) |
| `variationDetail` | the same five with a `Detail` suffix: `Detail[T]{Value, VariationIndex *int, Reason}` |
| `status` | `(*Client) Status() Status` |
| `close` | `(*Client) Close()` |
| `url`, `sdkKey` | `Options.URL`, `Options.SDKKey` (both required) |
| `stream`, `pollInterval`, `flushInterval`, `requestTimeout` | `Options.Stream *bool`, `Options.PollInterval`, `Options.FlushInterval`, `Options.RequestTimeout` (nil or zero means the default) |
| `hooks.afterEvaluation` | `Options.Hooks.AfterEvaluation func(Event)` |
| `onError` | `Options.OnError func(error)` |
| `accessor shape` | five typed accessors with a `...Detail` sibling, no generic method |

The full mapping table, with the reason for each row and the closed sets (`ReasonKind`, `ErrorCode`,
`StreamState`, `LastError`), is section 1 of [`DESIGN.md`](DESIGN.md). A `Reason` is
`{ kind, ruleIndex, flagKey, code }`: `ruleIndex` is nil unless the kind is `RULE_MATCH`, `flagKey`
names the flag (or the prerequisite) that decided it, and `code` is empty unless the kind is `ERROR`.

## Guarantees

1. It never panics at a caller. `New`, `Ready`, the five accessors and their `Detail` siblings,
   `Status` and `Close` return whatever the server, the network, the context, a hook or `OnError`
   does. A failure is the caller's fallback with an `ERROR` reason, or an `OnError` call, or both.
2. `variation` and `variationDetail` never do I/O and never wait. They read memory and run the
   evaluator. `Status` is the same.
3. `Ready` settles after the first attempt: true if a ruleset loaded, false if not, and a later load
   does not change the answer.
4. Last good ruleset. While the server is unreachable the SDK keeps evaluating from the ruleset it
   holds. Before the first load every evaluation is the fallback with `NOT_READY`.
5. Only a ruleset replaces a ruleset. An answer that is not a JSON object with `flags` and
   `segments` arrays is `bad_ruleset` and leaves the held ruleset in place.

The contract these come from is [`docs/sdk-contract.md`](../../docs/sdk-contract.md), section 5.

## Developing

The toolchain is pinned in [`mise.toml`](mise.toml): Go 1.27.4, golangci-lint 2.15.0 and zig 0.17.1
(zig is the C compiler `bin/test` gives cgo, so the race detector works where no system compiler is
installed). Install it once:

```sh
(cd sdks/go && mise trust && mise install)
```

From the repository root:

```sh
sdks/go/bin/lint    # gofmt -l and golangci-lint, no warnings allowed
sdks/go/bin/test    # builds, runs the tests under -race, prints "vectors: <n> pins: <n>"
```

The vectors are the shared ones in [`fixtures/evaluation`](../../fixtures/evaluation), read from
this repository rather than copied.

The conformance harness drives the SDK through its test service, which `bin/test` builds into
`build/testservice` (ignored by git):

```sh
bun run conformance -- --service sdks/go/bin/test-service
```

The service's own note, including the accessor mapping it advertises, is
[`test-service/README.md`](test-service/README.md).
