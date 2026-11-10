# ToggleSlash server SDK for Go

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It
meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md), has no dependency beyond the Go standard library
(Go 1.26 or later) and is **not published**: there is no module proxy entry to fetch it from.

## Quickstart

From a checkout of this repository, point your module at the directory:

```sh
go mod edit -require github.com/madangryscientist/toggleslash/sdks/go@v0.0.0
go mod edit -replace github.com/madangryscientist/toggleslash/sdks/go=../path/to/toggleslash/sdks/go
```

Then:

```go
package main

import (
	"os"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	client := toggleslash.Create("https://flags.example.com", os.Getenv("TOGGLESLASH_SDK_KEY"))
	defer client.Close() // sends the last usage counts and stops the two background goroutines

	client.Ready() // optional: waits for the first load, never longer than the request timeout
	user := toggleslash.Object(map[string]toggleslash.Value{
		"key":  toggleslash.String("user-42"),
		"plan": toggleslash.String("pro"),
	})
	if client.BoolVariation("new-checkout", user, false) {
		// ...
	}
}
```

`Create` returns at once and never fails. `Variation` and its typed siblings never wait for the network:
before the first load they answer your fallback with the reason `ERROR` / `NOT_READY`.

Read the key from the environment, never from source.

## The API

Every name in the contract and its Go spelling is in the table in [`DESIGN.md`](DESIGN.md), along with the
design choices behind it. In short:

| Contract | Go |
| --- | --- |
| `create` | `toggleslash.Create(url, sdkKey, opts...)` with `WithStream`, `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout`, `WithAfterEvaluation`, `WithOnError` |
| `ready` | `client.Ready()` returns a `bool` |
| `variation`, `variationDetail` | `client.BoolVariation(flagKey, ctx, fallback)` and `BoolVariationDetail`, and the same for `String`, `Number` (`float64`), `Int` (`int64`) and `JSON` |
| `status` | `client.Status()` returns a `Status` (`ETag`, `LastSyncAt`, `Stream`, `LastError`) |
| `close` | `client.Close()` |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(toggleslash.EvaluationEvent))` |
| `onError` | `WithOnError(func(error))` |

Durations are `time.Duration`. A context is a `toggleslash.Value` object (`Object`, `String`, `Number`,
`Bool`, `Array`, `Null`, or `ParseJSON` for JSON text). Reason kinds, error codes and stream states are
string types whose values are the contract's spelling (`toggleslash.ReasonRuleMatch == "RULE_MATCH"`).

`onError` is called on the SDK's own goroutine for a failed ruleset, stream or usage request, and on the
caller's goroutine for a hook that panicked or an evaluation that failed. Keep it short, and do not wait in
it for work that needs the SDK: it may call `Status`, `Ready`, an accessor and `Close`.

## Guarantees

The contract's section 5, as this SDK keeps it:

1. It never panics at a caller, whatever the server, the network, a context, a hook or `onError` does.
2. A `Variation` accessor and `Status` never do I/O and never wait.
3. `Ready` settles after the first attempt and gives the same answer every time.
4. While the server is unreachable it keeps evaluating from the last good ruleset.
5. Only something with the shape of a ruleset replaces a ruleset.

It also sends no context, context key or attribute anywhere: usage counts carry a flag, a variation index and
a day. A client that is never closed does not keep the process from exiting.

## Running the checks

The toolchain is pinned in [`mise.toml`](mise.toml) (`mise install` from this directory). The `bin` scripts
find it themselves.

```sh
sdks/go/bin/lint      # go vet, golangci-lint (.golangci.yml), gofmt -l
sdks/go/bin/test      # build, every unit test and every evaluation vector, under the race detector
bun run conformance -- --service sdks/go/bin/test-service   # from the repository root: the conformance harness
```

The race detector needs cgo and a C compiler. The test service is described in
[`test-service/README.md`](test-service/README.md). A report of the run that built this SDK is in
[`RUN.md`](RUN.md).
