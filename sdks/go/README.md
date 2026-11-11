# ToggleSlash server SDK for Go

The Go server SDK of [ToggleSlash](../../README.md). It follows the language-neutral
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md), against a ruleset it keeps current from the server. It uses the
standard library only (Go 1.26 or newer) and is not published anywhere: use it from a checkout of this
repository.

## Quickstart

The module is `github.com/madangryscientist/toggleslash/sdks/go` and its package is named `toggleslash`. From a
checkout, point your own module at the directory:

```sh
go mod edit -require github.com/madangryscientist/toggleslash/sdks/go@v0.0.0 \
  -replace github.com/madangryscientist/toggleslash/sdks/go=/path/to/checkout/sdks/go
```

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
	defer client.Close() // sends the pending usage counts and stops the background goroutines

	if client.Ready() { // waits for the first load attempt, bounded by the request timeout
		fmt.Println("ruleset loaded")
	}

	context := map[string]any{"key": "user-42", "plan": "pro"}
	if client.BoolVariation("new-checkout", context, false) { // the fallback fixes the expected kind
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner-text", context, "welcome")
	fmt.Println(detail.Value, detail.VariationIndex, detail.Reason)
}
```

Options go after the two arguments: `toggleslash.WithStream(false)`, `WithPollInterval`, `WithFlushInterval`,
`WithRequestTimeout` (all `time.Duration`), `WithAfterEvaluation` and `WithOnError`.

## Mapping from the contract

The full table (every name, option and default, and what is a `Detail`, a `Status` and a `StreamState`) is
section 1 of [`DESIGN.md`](DESIGN.md). In short: `New`, `client.Ready()`, `client.BoolVariation(...)` and its
`String`, `Int` (int64, the integer accessor), `Float` and `JSON` siblings, each with a `...Detail` twin,
`client.Status()` and `client.Close()`. Go methods cannot be generic, so there is one accessor per kind.

## Guarantees

- Nothing panics or errors at a caller: a failure becomes your fallback with an `ERROR` reason, goes to `WithOnError`, or both.
- `Variation*` and `Status` read memory only: they never wait on the network, a load or a flush.
- `Ready()` settles after the first attempt, is bounded by the request timeout, and gives the same answer each time.
- The SDK keeps evaluating from the last good ruleset while the server is unreachable; only a valid ruleset replaces one.
- No evaluation context, attribute or SDK key appears in a request body or URL (the key is the `Authorization`
  header only), a log line, a status or an error the SDK builds.
- `Close()` is safe to call twice and from many goroutines, and returns within about twice the request timeout.
- A client that is never closed does not keep the process alive.

## Check it

From the root of the repository:

```sh
sdks/go/bin/lint                                            # go vet, golangci-lint (.golangci.yml) and gofmt -l
sdks/go/bin/test                                            # go test -race, every evaluation vector, and the test service build
bun run conformance -- --service sdks/go/bin/test-service   # the shared conformance harness
```

`mise.toml` pins Go, golangci-lint and zig (the race detector needs a C compiler, and `zig cc` is one); with
`mise` installed the scripts use those versions. See [`test-service/README.md`](test-service/README.md) for the
conformance service, [`DESIGN.md`](DESIGN.md) for the design decisions and [`RUN.md`](RUN.md) for the report of the
run that wrote this SDK.
