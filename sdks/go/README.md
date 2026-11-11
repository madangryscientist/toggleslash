# ToggleSlash server SDK for Go

The Go server SDK of [ToggleSlash](../../README.md). It follows the language-neutral
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md), against a ruleset it keeps current from the server. It uses the
standard library only (Go 1.26 or newer) and is not published anywhere: use it from a checkout of this repository.

## Quickstart

Point your module at a checkout, with no tag and no registry:

```sh
go mod edit -require=github.com/madangryscientist/toggleslash/sdks/go@v0.0.0
go mod edit -replace=github.com/madangryscientist/toggleslash/sdks/go=/path/to/checkout/sdks/go
```

```go
package main

import (
	"fmt"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
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

	context := jsonvalue.From(map[string]any{"key": "user-42", "plan": "pro"})
	if client.BoolVariation("new-checkout", context, false) { // the fallback fixes the expected kind
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner-text", context, "welcome")
	fmt.Println(detail.Value, detail.VariationIndex, detail.Reason.Kind)
}
```

`jsonvalue` is a small JSON value type the SDK takes contexts and fallbacks in and returns values as. It exists
because `encoding/json` cannot hold what the evaluation spec requires (`1e999` is Infinity, and a lone surrogate in
a string is kept). `jsonvalue.From` builds one from `map[string]any`, `[]any`, strings, numbers, bools and nil.

## Mapping from the contract

The full table (every name, option and default, and what is a `Detail`, a `Status` and a stream state) is
section 1 of [`DESIGN.md`](DESIGN.md). In short: `toggleslash.New`, `client.Ready()`,
`client.Variation(...)` and the typed `BoolVariation`, `StringVariation`, `FloatVariation`, `IntVariation` (the
integer accessor) and `JSONVariation`, each with a `...Detail` form, `client.Status()`, `client.Close()`; options
`WithStream`, `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout`, `WithAfterEvaluation` and `WithOnError`.

## Guarantees

- Nothing panics or errors at a caller: a failure becomes your fallback with an `ERROR` reason, goes to `WithOnError`, or both.
- `Variation` and its forms, and `Status`, read memory only: they never wait on the network, a load or a flush.
- `Ready` settles after the first attempt, is bounded by the request timeout, and gives the same answer each time.
- The SDK keeps evaluating from the last good ruleset while the server is unreachable; only a valid ruleset replaces one.
- No evaluation context, attribute or SDK key appears in a request body or URL (the key is the `Authorization`
  header only), a log line, a status or an error the SDK builds.
- `Close` is safe to call twice and from many goroutines, and returns within about twice the request timeout.
- The tests run under the race detector.

## Check it

From the root of the repository:

```sh
sdks/go/bin/lint                                            # gofmt, go vet and golangci-lint, no warnings
sdks/go/bin/test                                            # go test -race, and every evaluation vector
bun run conformance -- --service sdks/go/bin/test-service   # the shared conformance harness
```

`mise.toml` pins Go, zig and golangci-lint; with `mise` installed the scripts use those versions. The race detector
needs a C compiler, so `bin/test` uses the pinned zig as `zig cc`. See [`test-service/README.md`](test-service/README.md)
for the conformance service, [`DESIGN.md`](DESIGN.md) for the design decisions and [`RUN.md`](RUN.md) for the
report of the run that wrote this SDK.
