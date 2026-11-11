# Go server SDK for ToggleSlash

Not published. Use it from a checkout of this repository. It is written to
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates to [`docs/evaluation.md`](../../docs/evaluation.md).
[`DESIGN.md`](DESIGN.md) holds the design and the mapping of every canonical name to Go.

Module `github.com/madangryscientist/toggleslash/sdks/go`, package `toggleslash`. It needs Go 1.26 or newer
and no dependency outside the standard library.

## Quickstart

In another module, point at the checkout:

```sh
go mod edit -require github.com/madangryscientist/toggleslash/sdks/go@v0.0.0 \
  -replace github.com/madangryscientist/toggleslash/sdks/go=/path/to/toggleslash/sdks/go
```

```go
package main

import (
	"fmt"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	client := toggleslash.New("https://flags.example.test", "fs_server_...",
		toggleslash.WithOnError(func(err error) { fmt.Println("toggleslash:", err) }))
	defer client.Close()

	client.Ready() // waits for the first attempt, at most about the request timeout

	user := toggleslash.UserContext("user-42", map[string]any{"plan": "pro"})
	if client.BoolVariation("new-checkout", user, false) {
		fmt.Println("new checkout")
	}
	detail := client.StringVariationDetail("banner", user, "none")
	fmt.Println(detail.Value, detail.Reason.Kind)
}
```

`New` never fails and never blocks: it starts the loops and returns. `Variation` and `Status` never wait
for the network.

## Mapping

| Canonical | Go |
| --- | --- |
| `create` | `New(url, sdkKey, opts...)` |
| `ready` | `Ready() bool` |
| `variation` | `Variation`, `BoolVariation`, `StringVariation`, `NumberVariation`, `IntVariation`, `JSONVariation` |
| `variationDetail` | the same names with `Detail` after them |
| `status` | `Status()` |
| `close` | `Close()` |
| `stream`, `pollInterval`, `flushInterval`, `requestTimeout` | `WithStream`, `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout` |
| `hooks.afterEvaluation`, `onError` | `WithAfterEvaluation`, `WithOnError` |

The rest of the table (types, closed sets, threads) is in [`DESIGN.md`](DESIGN.md) section 1.

## Guarantees

- No call panics or returns an error to the caller. A failure is a result with a reason, a `status`, or an `onError` call.
- `Variation` and `Status` do no I/O and wait for nothing.
- The last good ruleset is kept: a failed or malformed answer never replaces it.
- The ruleset and its ETag swap together, so a hook's `etag` and `flagVersion` always match.
- Callbacks run with no SDK lock held, so they may call back into the client.
- `Close` is idempotent, safe from any goroutine, sends the last usage counts and returns within about twice the request timeout.
- No end-user context leaves the process: requests carry usage counts only, and the SDK logs nothing.
- An unclosed client does not keep the process alive.

## Develop

```sh
sdks/go/bin/lint                                               # gofmt, go vet, golangci-lint
sdks/go/bin/test                                               # go test -race, then prints the vector and pin counts
bun run conformance -- --service sdks/go/bin/test-service      # the shared scenarios and vectors
```

`mise install` in this directory installs Go, golangci-lint and zig. The race detector needs cgo, which
needs a C compiler, and a machine may not have one: `mise.toml` pins `zig`, and `bin/test` sets
`CC` to the pinned `zig cc` by path.
