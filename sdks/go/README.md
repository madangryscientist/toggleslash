# ToggleSlash server SDK for Go

The Go server SDK of [ToggleSlash](../../README.md). It follows the language-neutral
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md), against a ruleset it keeps current from the server. It has no
runtime dependency beyond the Go standard library (Go 1.26 or newer) and is not published anywhere: use it from
a checkout of this repository, for example with a `replace` directive in your `go.mod`:

```
replace github.com/madangryscientist/toggleslash/sdks/go => ../path/to/checkout/sdks/go
```

## Quickstart

```go
package main

import (
	"fmt"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	client := toggleslash.New(
		"https://toggleslash.example.invalid", // the server's base URL
		"sdk-key-from-the-dashboard",          // a server SDK key
	)
	defer client.Close() // flushes usage counts and stops the background goroutines

	if client.Ready() { // waits for the first load attempt, bounded by the request timeout
		fmt.Println("ruleset loaded")
	}

	user := toggleslash.NewContext("user", "user-42", map[string]any{"plan": "pro"})
	if client.BoolVariation("new-checkout", user, false) { // the fallback fixes the expected kind
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner-text", user, "welcome")
	fmt.Println(detail.Value, detail.VariationIndex, detail.Reason.Kind)
}
```

Options go after the key: `WithStream`, `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout`,
`WithAfterEvaluation` and `WithOnError`.

## Mapping from the contract

The table that maps every name in the contract onto this package (operations, options, reason kinds, error
codes, status fields) is in [`DESIGN.md`](DESIGN.md), section 1. The test service's accessor mapping is in
[`test-service/README.md`](test-service/README.md).

## What it guarantees

From section 5 of the contract:

- It never panics at a caller. `New`, `Ready`, the variation methods, `Status` and `Close` absorb every failure
  of the server, the network, a hook or the `onError` function. A failure becomes the caller's fallback with an
  `ERROR` reason, a call to `onError`, or both.
- The variation methods and `Status` never do I/O and never wait. They read memory and run the evaluator.
- `Ready` settles after the first load attempt, is bounded by the request timeout, and gives the same answer
  every time after.
- While the server is unreachable it keeps evaluating from the last good ruleset. Before any load, every
  evaluation is the fallback with `NOT_READY`.
- Only a ruleset replaces a ruleset: a bad gateway page, `{}` or a truncated body is an error and changes
  nothing.
- No context, context key or flag value is ever sent to the server; usage is counted per flag, variation and
  UTC day.

## Checks

From the root of a checkout, after `mise install` (it pins Go, zig and golangci-lint in
[`mise.toml`](mise.toml)):

```sh
sdks/go/bin/lint                                          # golangci-lint, gofmt and go vet
sdks/go/bin/test                                          # go vet, go test -race, the vector count, the test service build
bun run conformance -- --service sdks/go/bin/test-service # the harness in packages/sdk-conformance
```

`go test -race` needs a C compiler. The scripts use the pinned zig as one (`zig cc`), so no system compiler is
needed. CI runs the same three commands in `.github/workflows/sdk-go.yml`.
