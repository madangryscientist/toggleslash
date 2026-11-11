# ToggleSlash Go server SDK

A server SDK for ToggleSlash in Go, built from [`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md)
and held to the [SDK contract](../../docs/sdk-contract.md). It loads the ruleset, follows the stream (with a
polling fallback), evaluates locally with its own copy of the evaluator, and batches usage counts. It has
no dependency outside the Go standard library. It is not published anywhere: use it from a checkout of
this repository.

## Quickstart

From a checkout, point a module at the directory (no tag, no registry):

```sh
go mod edit -require github.com/madangryscientist/toggleslash/sdks/go@v0.0.0
go mod edit -replace github.com/madangryscientist/toggleslash/sdks/go=/path/to/toggleslash/sdks/go
```

```go
package main

import (
	"context"
	"fmt"
	"time"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	// New returns at once and never fails; the first load runs in the background.
	client := toggleslash.New("http://localhost:5000", "ts_sdk_example_key")
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client.Ready(ctx) // true when a ruleset loaded; every call below works either way

	user := toggleslash.NewUserContext("user-42")
	if client.BoolVariation("new-checkout", user, false) {
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner", user, "none")
	fmt.Println(detail.Value, detail.Reason.Kind)
}
```

Options are functional: `WithStream(false)`, `WithPollInterval`, `WithFlushInterval`,
`WithRequestTimeout`, `WithAfterEvaluation` and `WithOnError`. The Go names for every contract name are
in [`DESIGN.md`](DESIGN.md), section 1 (the mapping table).

Accessors are typed methods: `BoolVariation`, `StringVariation`, `IntVariation` (`int64`),
`FloatVariation` (`float64`) and `JSONVariation`, each with a `Detail` form that returns the value, the
variation index and the reason. A context is built with `NewUserContext`, `ContextFrom` (a map, for other
kinds and attributes) or `ParseContext` (JSON).

## Guarantees

The contract's section 5, as they hold here:

- **Never panics at a caller.** `New`, `Ready`, every accessor, `Status` and `Close` recover from
  whatever the server, the network, the context, a hook or `OnError` does. A failure becomes the fallback
  with an `ERROR` reason, or an `OnError` call, or both.
- **Accessors and `Status` never do I/O and never wait.** They read memory and run the evaluator; a hung
  ruleset request or a hung usage flush does not slow them.
- **`Ready` settles after the first attempt**, bounded by the request timeout, and gives the same answer
  every time after.
- **Last good ruleset.** While the server is unreachable the SDK keeps evaluating from the ruleset it
  holds. Before any load every evaluation is the fallback with `NOT_READY`.
- **Only a ruleset replaces a ruleset.** An answer that is not an object with `flags` and `segments`
  arrays is an error and changes nothing.

It also keeps the end user private: the context never leaves the process, and usage counts carry a flag
key, a variation and a day, nothing else.

## Checks

Install the toolchain from `mise.toml` (Go, golangci-lint and zig) and run from the repository root:

```sh
(cd sdks/go && mise trust && mise install)
bun install
sdks/go/bin/lint
sdks/go/bin/test
bun run conformance -- --service sdks/go/bin/test-service
```

`bin/lint` runs `gofmt`, `go vet` and `golangci-lint` (rule set in `.golangci.yml`). `bin/test` runs the
package tests under the race detector, which needs cgo and so a C compiler: it uses `cc` when there is
one and the pinned zig (`zig cc`) otherwise. The tests include every evaluation vector in
`fixtures/evaluation/` and the pins in `internal/evaluator`. `bin/test-service` builds and runs the
conformance test service in `test-service/`, see [its README](test-service/README.md).

The report of the run that built this SDK, with its decisions and guesses, is in [`RUN.md`](RUN.md).
