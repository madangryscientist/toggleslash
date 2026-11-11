# ToggleSlash server SDK for Go

The Go server SDK for ToggleSlash. It evaluates flags locally, to [`docs/evaluation.md`](../../docs/evaluation.md),
over a ruleset it keeps current from the server, and keeps the contract in
[`docs/sdk-contract.md`](../../docs/sdk-contract.md). Module
`github.com/madangryscientist/toggleslash/sdks/go`, package `toggleslash`. Nothing is published and no
registry is named: use it from a checkout of this repository.

## Quickstart

From a checkout, with no tag, point a `replace` at the directory:

```text
require github.com/madangryscientist/toggleslash/sdks/go v0.0.0
replace github.com/madangryscientist/toggleslash/sdks/go => /path/to/toggleslash/sdks/go
```

```go
package main

import (
	"fmt"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	client := toggleslash.New("https://flags.example.test", "sdk-key-from-the-environment")
	defer client.Close()

	client.Ready() // optional: waits for the first load, bounded by the request timeout

	user := toggleslash.UserContext("u1", map[string]toggleslash.Value{
		"plan": toggleslash.String("pro"),
	})
	if client.BoolVariation("new-checkout", user, false) {
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner", user, "none")
	fmt.Println(detail.Value, detail.VariationIndex, detail.Reason.Kind)
}
```

`New` returns at once. Before the first load, and when a flag is missing, malformed or of another type, an
accessor answers the fallback, and the `Detail` form says why.

## The mapping

The canonical names of the contract map to Go in the table in [`DESIGN.md`](DESIGN.md#1-the-mapping-table),
with the closed sets (reason kinds, error codes, stream states, `lastError` codes) and their spelling.
`DESIGN.md` also holds the concurrency model, the HTTP and stream handling, the dependencies (none beyond the
standard library) and the package layout.

## Guarantees

From contract section 5:

- No operation raises or panics at the caller, whatever the server, the network or the context does.
- An evaluation never waits on the network: it reads the ruleset held, or answers `NOT_READY` before the first load.
- `Ready` settles after the first attempt, bounded, and may be called again.
- An answer that is not a ruleset never replaces the one held, and a ruleset and its ETag are swapped together.
- Every operation is safe from many goroutines at once, and `Close` is safe to call twice or concurrently.
- A callback that panics, or calls back into the client, neither breaks the client nor deadlocks it.
- No context, context key or attribute appears in a request, an error or a log line.
- Usage counts are batched, kept through a failure the server may recover from, and flushed once more on `Close`.

## Toolchain and checks

```sh
(cd sdks/go && mise trust && mise install)   # go, golangci-lint and zig, as sdks/go/mise.toml pins them

sdks/go/bin/lint            # gofmt, go vet and golangci-lint, no warnings allowed
sdks/go/bin/test            # native tests under the race detector, every evaluation vector, and the test service build
bun run conformance -- --service sdks/go/bin/test-service    # from the repository root, after bun install
```

The race detector needs cgo, and the machine is not assumed to have a C compiler, so `mise.toml` pins `zig`
and `bin/test` uses `zig cc` by path. `sdks/go/test-service` is the conformance test service; see its
[`README.md`](test-service/README.md) for the accessor mapping.
