# ToggleSlash server SDK for Go

The Go server SDK of [ToggleSlash](../../README.md). It follows the language-neutral
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md), against a ruleset it keeps current from the server. It uses
the standard library only and is not published anywhere: no module is tagged and no registry holds it. Use it
from a checkout of this repository.

## Quickstart

From your own module, point at a checkout (there is no tag to ask for):

```sh
go mod edit -require=github.com/madangryscientist/toggleslash/sdks/go@v0.0.0
go mod edit -replace=github.com/madangryscientist/toggleslash/sdks/go=/path/to/checkout/sdks/go
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
	defer client.Close() // flushes usage counts and stops the SDK's goroutines

	if client.Ready() { // waits for the first load attempt, bounded by the request timeout
		fmt.Println("ruleset loaded")
	}

	user := toggleslash.NewContext("user-42", map[string]any{"plan": "pro"})
	if client.BoolVariation("new-checkout", user, false) { // the fallback fixes the expected kind
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner-text", user, "welcome")
	fmt.Println(detail.Value, detail.VariationIndex, detail.Reason.Kind)
}
```

## Mapping from the contract

The full table (every name, option and default, and what is a `Detail`, a `Status` and a `StreamState`) is
section 1 of [`DESIGN.md`](DESIGN.md). In short: `toggleslash.New`, `client.Ready()`, `client.Variation(...)`,
`client.VariationDetail(...)` and the typed `Bool`, `String`, `Float64` and `Int64` variants of each (`Int64` is
the integer accessor), `client.Status()`, `client.Close()`; options `WithStream`, `WithPollInterval`,
`WithFlushInterval`, `WithRequestTimeout`, `WithAfterEvaluation` and `WithOnError`.

## Guarantees

- Nothing panics at a caller: a failure becomes your fallback with an `ERROR` reason, goes to the `WithOnError`
  function, or both.
- `Variation*` and `Status` read memory only: they never wait on the network, a load or a flush.
- `Ready()` settles after the first attempt, is bounded by the request timeout, and gives the same answer each time.
- The SDK keeps evaluating from the last good ruleset while the server is unreachable; only a valid ruleset
  replaces one.
- No evaluation context, attribute or SDK key appears in a request body or URL (the key is the `Authorization`
  header only), a log line, a status or an error the SDK builds.
- `Close()` is safe to call twice and from many goroutines, and returns within about twice the request timeout.
- A client that is never closed does not keep the process alive.

## Check it

From the root of the repository (the scripts run Go through `mise`, which pins Go, zig and golangci-lint in
`mise.toml`; zig is the C compiler `go test -race` needs):

```sh
sdks/go/bin/lint                                            # gofmt, go vet and golangci-lint
sdks/go/bin/test                                            # go test -race, every evaluation vector, and the test-service build
bun run conformance -- --service sdks/go/bin/test-service   # the shared conformance harness
```

See [`test-service/README.md`](test-service/README.md) for the conformance service, [`DESIGN.md`](DESIGN.md)
for the design decisions and [`RUN.md`](RUN.md) for the report of the run that wrote this SDK.
