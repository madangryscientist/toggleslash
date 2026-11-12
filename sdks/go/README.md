# ToggleSlash server SDK for Go

The Go server SDK of [ToggleSlash](../../README.md). It follows the language-neutral
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md), against a ruleset it keeps current from the server. It has no
dependency outside the Go standard library (Go 1.26 or newer) and is not published anywhere: use it from a
checkout of this repository.

## Quickstart

From a checkout of this repository (no tag, no registry), point your module at the directory:

```sh
go mod edit -require=github.com/madangryscientist/toggleslash/sdks/go@v0.0.0 \
  -replace=github.com/madangryscientist/toggleslash/sdks/go=path/to/checkout/sdks/go
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
	defer client.Close() // sends the usage counts and stops the background goroutines

	if client.Ready() { // waits for the first load attempt, bounded by the request timeout
		fmt.Println("ruleset loaded")
	}

	ctx := toggleslash.NewContext("user-42", map[string]any{"plan": "pro"})
	if client.BoolVariation("new-checkout", ctx, false) { // the fallback fixes the expected kind
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner-text", ctx, "welcome")
	fmt.Println(detail.Value, detail.VariationIndex, detail.Reason.Kind)
}
```

## Mapping from the contract

The full table (every name, option and default, and what is a `Detail`, a `Status` and a `StreamState`) is
section 1 of [`DESIGN.md`](DESIGN.md). In short:

| Contract | Go |
| --- | --- |
| `create` | `toggleslash.New(url, sdkKey, opts...)` |
| `ready` | `client.Ready() bool` |
| `variation` | `client.BoolVariation`, `StringVariation`, `Float64Variation`, `IntVariation` (an `int64`) and `JSONVariation`, each `(flagKey, ctx, fallback)` |
| `variationDetail` | the same names with `Detail` on the end, returning `Detail[T]` |
| `status` | `client.Status()` |
| `close` | `client.Close()` |
| `stream`, `pollInterval`, `flushInterval`, `requestTimeout` | `WithStream`, `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout` |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(Event))` |
| `onError` | `WithOnError(func(error))` |

A context is built with `NewContext`, `NewContextKind`, `NewMultiContext` or `ParseContext` (raw JSON, the form
that keeps `1e999` and a lone surrogate exactly).

## Guarantees

- Nothing panics at a caller: a failure becomes your fallback with an `ERROR` reason, goes to the `onError` function, or both.
- Evaluation and `Status` read memory only: they never wait on the network, a load or a flush.
- `Ready` settles after the first attempt, is bounded by the request timeout, and gives the same answer each time.
- The SDK keeps evaluating from the last good ruleset while the server is unreachable; only a valid ruleset replaces one.
- No evaluation context, attribute or SDK key appears in a request body or URL (the key is the `Authorization`
  header only), a log line (the SDK writes none), a status or an error the SDK builds.
- `Close` is safe to call twice and from many goroutines, and returns within about twice the request timeout.
- A client that is never closed does not keep the process from exiting.

## Check it

Install the toolchain once, then run from the root of the repository:

```sh
(cd sdks/go && mise trust && mise install)
sdks/go/bin/lint                                            # go vet, golangci-lint and gofmt, no warnings allowed
sdks/go/bin/test                                            # go test -race, and every evaluation vector
bun run conformance -- --service sdks/go/bin/test-service   # the shared conformance harness
```

`mise.toml` pins Go, golangci-lint and Zig (the race detector needs a C compiler, and `bin/test` names the
pinned Zig as one); with `mise` installed the scripts use those versions. See
[`test-service/README.md`](test-service/README.md) for the conformance service, [`DESIGN.md`](DESIGN.md) for the
design decisions and [`RUN.md`](RUN.md) for the report of the run that wrote this SDK.
