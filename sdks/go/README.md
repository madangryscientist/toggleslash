# ToggleSlash server SDK for Go

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It
meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md), has no dependency outside the Go standard
library (Go 1.26 or later) and is **not published**: there is no tag and no module proxy entry to install from.

## Quickstart

Install from a checkout of this repository, with a `replace` in your own `go.mod`:

```sh
go mod edit -require github.com/madangryscientist/toggleslash/sdks/go@v0.0.0 \
  -replace github.com/madangryscientist/toggleslash/sdks/go=/path/to/checkout/sdks/go
```

Then:

```go
package main

import (
	"os"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	client := toggleslash.New("https://flags.example.com", os.Getenv("TOGGLESLASH_SDK_KEY"))
	defer client.Close() // sends the last usage counts and stops the background goroutines

	client.Ready() // optional: waits for the first load, never longer than the request timeout
	ctx := toggleslash.UserContext("user-42").WithAttribute("plan", toggleslash.String("pro"))
	if client.BoolVariation("new-checkout", ctx, false) {
		// ...
	}
}
```

`New` returns at once and never panics. `...Variation` and its `...VariationDetail` twins never wait for the
network: before the first load they answer your fallback with the reason `ERROR` / `NOT_READY`. A client is
safe to use from many goroutines. Options are `WithStream`, `WithPollInterval`, `WithFlushInterval`,
`WithRequestTimeout`, `WithAfterEvaluation` and `WithOnError`.

Read the key from the environment, never from source.

## The API

Every name in the contract and its Go spelling is in the table in
[`DESIGN.md`](DESIGN.md#the-mapping-table), along with the design choices behind it.

## Guarantees

The contract's section 5, as this SDK keeps it:

1. It never panics at a caller, whatever the server, the network, a context, a hook or `OnError` does.
2. `Variation`, `VariationDetail` and `Status` never do I/O and never wait.
3. `Ready` settles after the first attempt and gives the same answer every time.
4. While the server is unreachable it keeps evaluating from the last good ruleset.
5. Only something with the shape of a ruleset replaces a ruleset.

It also sends no context, context key or attribute anywhere: usage counts carry a flag, a variation index and
a day.

## Running the checks

The toolchain is pinned in [`mise.toml`](mise.toml) (`mise install` from this directory). The `bin` scripts
find it themselves.

```sh
sdks/go/bin/lint      # gofmt -l, go vet, golangci-lint run
sdks/go/bin/test      # go vet, go test -race for every package and vector, then builds the test service
bun run conformance -- --service sdks/go/bin/test-service   # from the repository root: the conformance harness
```

The test service is described in [`test-service/README.md`](test-service/README.md). A report of the run that
built this SDK is in [`RUN.md`](RUN.md).
