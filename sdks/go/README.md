# ToggleSlash server SDK for Go

A server SDK for ToggleSlash, built from [`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md)
and held to the [SDK contract](../../docs/sdk-contract.md). It keeps one environment's flag rules in memory,
follows the server's stream (or polls), and evaluates flags locally with no I/O. It uses the standard library
only.

Nothing is published. The module path `github.com/madangryscientist/toggleslash/sdks/go` is a name for this
directory and is registered nowhere; the package is called `toggleslash`.

## Quickstart

Use it from a checkout of this repository, with a `replace` directive in your own `go.mod`:

```
require github.com/madangryscientist/toggleslash/sdks/go v0.0.0

replace github.com/madangryscientist/toggleslash/sdks/go => /path/to/toggleslash/sdks/go
```

```go
package main

import (
	"fmt"
	"os"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	client := toggleslash.New("http://localhost:5000", os.Getenv("TOGGLESLASH_SDK_KEY"))
	defer client.Close() // sends the pending usage counts

	client.Ready() // optional: waits for the first load (bounded by the request timeout)

	user := toggleslash.NewContext("user-123", map[string]any{"plan": "pro"})

	if client.BoolVariation("new-checkout", user, false) {
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner", user, "none")
	fmt.Println(detail.Value, detail.Reason.Kind, detail.VariationIndex)
}
```

`New` returns at once and never fails; a bad URL or an unreachable server shows up in `client.Status()` and
`WithOnError`, and every evaluation answers the fallback with `ERROR NOT_READY` until a ruleset is held.

## Mapping table

The contract's names, one row each, with their Go spelling and defaults: see
[`DESIGN.md`](DESIGN.md#the-mapping-table). In short: `create` is `toggleslash.New(url, sdkKey, opts...)`,
the options are `WithStream`, `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout`,
`WithAfterEvaluation`, `WithOnError` and `WithTransport`, the accessors are `BoolVariation`, `StringVariation`,
`IntVariation` (`int64`), `FloatVariation` and `JSONVariation` with a `...Detail` twin each, and the generic
`Variation[T]` and `VariationDetail[T]`.

## Guarantees

From contract section 5, and each one tested:

- Nothing the server, the network, a context, a hook or `onError` does makes an operation panic at a caller.
- An evaluation never waits on the network: before the first load it answers the fallback with `NOT_READY`.
- `Ready` settles after the first attempt, success or not, and may be called again.
- A body that is not a ruleset never replaces the one held, and the ruleset and its ETag change together.
- A request is bounded by `requestTimeout`, body included; a stream that is silent for 60 s is dropped.
- `Close` is safe to call twice or at once, sends the pending usage counts, and returns in bounded time.
- Usage is counts per flag, variation and UTC day. No context, key or attribute leaves the process.

## Checks

From the repository root, with the toolchain in [`mise.toml`](mise.toml) installed
(`(cd sdks/go && mise trust && mise install)`) and `bun install` run once:

```sh
sdks/go/bin/lint                                          # gofmt -s, go vet, golangci-lint
sdks/go/bin/test                                          # go test -race, every evaluation vector included
bun run conformance -- --service sdks/go/bin/test-service # the shared SDK conformance scenarios
```

Go's race detector needs a C compiler. `mise.toml` pins zig, and `bin/test` uses it as `CC` by path, so
only the pinned toolchain is needed. The minimum Go is 1.26; development is on 1.27.4.
