# ToggleSlash server SDK for Go

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It meets
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), has **no runtime dependency** beyond the Go standard
library, and is **not published**: there is no tag and no module registry entry to `go get`.

## Quickstart

Use it from a checkout of this repository with a `replace` directive in your own `go.mod` (the module path is a
name only; nothing is registered under it):

```
require github.com/madangryscientist/toggleslash/sdks/go v0.0.0
replace github.com/madangryscientist/toggleslash/sdks/go => /path/to/toggleslash/sdks/go
```

Then:

```go
package main

import (
	"os"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	client := toggleslash.NewClient("https://flags.example.com", os.Getenv("TOGGLESLASH_SDK_KEY"))
	defer client.Close() // sends the last usage counts and stops the two background goroutines

	client.Ready() // optional: waits for the first load, never longer than the request timeout

	user := toggleslash.Object(
		toggleslash.Member{Key: "key", Value: toggleslash.String("user-42")},
		toggleslash.Member{Key: "plan", Value: toggleslash.String("pro")},
	)
	if client.BoolVariation("new-checkout", user, false) {
		// ...
	}
}
```

`NewClient` returns at once and never panics or returns an error. A variation never waits for the network:
before the first load it answers your fallback with the reason `ERROR` / `NOT_READY`. Read the key from the
environment, never from source. Call `Close` when you are done; a client that is never closed leaks two
goroutines and nothing else, and does not keep the process from exiting.

## The API

Every name in the contract and its Go spelling is in the table in [`DESIGN.md`](DESIGN.md#1-the-mapping-table),
with the design choices behind it. In short:

| Contract | Go |
| --- | --- |
| `create` | `toggleslash.NewClient(url, sdkKey, opts ...Option) *Client` |
| options | `WithStream(bool)`, `WithPollInterval(d)`, `WithFlushInterval(d)`, `WithRequestTimeout(d)`, `WithAfterEvaluation(f)`, `WithOnError(f)`, `WithHTTPClient(c)` |
| `ready` | `client.Ready() bool` |
| `variation` | `BoolVariation`, `StringVariation`, `IntVariation` (int64), `FloatVariation` (float64), `JSONVariation` (`Value`) |
| `variationDetail` | the same five names plus `Detail`, each returning `Detail[T]{Value, VariationIndex, Reason}` |
| `status` | `client.Status() Status{ETag, LastSyncAt, Stream, LastError}`; an empty `ETag`, an empty `LastError` and a zero `LastSyncAt` are "none"; `LastError` is a `SyncError` |
| `close` | `client.Close()` |

A context is a `toggleslash.Context`, the SDK's own JSON value: build it with `Object`, `String`, `Number`,
`Bool`, `Null` and `Array`, with `ValueOf` from ordinary Go values, or with `ParseJSON` from raw JSON (which
keeps `1e999` and lone surrogates, as `docs/evaluation.md` requires and `encoding/json` cannot).

## Guarantees

The contract's section 5, as this SDK keeps it:

1. It never panics at a caller, whatever the server, the network, a context, a hook or `OnError` does.
2. Variations and `Status` never do I/O and never wait on it.
3. `Ready` settles after the first attempt and gives the same answer every time.
4. While the server is unreachable it keeps evaluating from the last good ruleset.
5. Only something with the shape of a ruleset replaces a ruleset.

It also sends no context, context key or attribute anywhere: usage counts carry a flag, a variation index and
a day. Redirects are not followed, and the SDK key is sent only to the URL you gave.

## Running the checks

The toolchain is pinned in [`mise.toml`](mise.toml) (`mise install` from this directory): Go, golangci-lint and
zig. The race detector needs cgo, so **a C compiler must exist**; `bin/test` uses the `zig cc` that mise
installs when `CC` is not set, so no system compiler is needed. The `bin` scripts find the toolchain
themselves.

```sh
sdks/go/bin/lint      # gofmt -l, go vet and golangci-lint; reads, never rewrites
sdks/go/bin/test      # build, then every unit test and every evaluation vector under -race
bun run conformance -- --service sdks/go/bin/test-service   # from the repository root: the conformance harness
```

The test service is described in [`test-service/README.md`](test-service/README.md). A report of the run that
built this SDK is in [`RUN.md`](RUN.md).
