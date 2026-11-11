# ToggleSlash server SDK for Go

A server-side SDK for ToggleSlash feature flags. It loads the ruleset from a ToggleSlash server, follows
live changes, evaluates flags locally and reports usage counts. It is built from
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md) and follows
[`docs/sdk-contract.md`](../../docs/sdk-contract.md). Nothing is published: use it from a checkout of this
repository.

Go 1.26 or newer. It has no dependency outside the standard library.

## Quickstart

From a checkout, point a module at the directory:

```bash
go mod edit -replace github.com/madangryscientist/toggleslash/sdks/go=/path/to/toggleslash/sdks/go
go get github.com/madangryscientist/toggleslash/sdks/go
```

```go
package main

import (
	"fmt"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	client := toggleslash.New("https://flags.example.com", "SERVER_SDK_KEY")
	defer client.Close()

	client.Ready() // true once the first ruleset has loaded; bounded by the request timeout

	user := toggleslash.NewContext("user-42").With("plan", "pro")
	if client.BoolVariation("new-checkout", user, false) {
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner", user, "none")
	fmt.Println(detail.Value, detail.Reason.Kind)
}
```

`New` returns at once and starts loading in the background. `Close` sends the last usage counts. A
client that is never closed does not keep the process alive.

## The API

| Contract | Go |
| --- | --- |
| create | `toggleslash.New(url, sdkKey, options...)` |
| ready | `client.Ready()`, `client.ReadyContext(ctx)` |
| variation | `BoolVariation`, `StringVariation`, `IntVariation`, `FloatVariation`, `JSONVariation` |
| variationDetail | the same names ending in `Detail`, returning `Detail[T]{Value, VariationIndex, Reason}` |
| status | `client.Status()` |
| close | `client.Close()` |
| options | `WithStream`, `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout`, `WithAfterEvaluation`, `WithOnError`, plus `WithLogger` and `WithHTTPClient` |

[`DESIGN.md`](DESIGN.md) has the full mapping, the closed sets and the reasons.

## Guarantees

- It never panics at a caller: a failure becomes the fallback, a reason and an `OnError` call.
- `Variation` and `Status` read memory and do no I/O.
- `Ready` settles after the first load attempt, within the request timeout, and does not change after.
- The last good ruleset is kept; an answer that is not a ruleset replaces nothing.
- Only usage counts leave the process: no context, no context key, nothing about an end user.
- Callbacks run without any SDK lock held, so they may call back into the client.
- `Close` is safe to call twice or from many goroutines, and returns within about twice the request timeout.

## Checks

```bash
sdks/go/bin/lint                                             # gofmt, go vet, golangci-lint
sdks/go/bin/test                                             # go test -race, and the evaluation vector count
bun run conformance -- --service sdks/go/bin/test-service    # from the repository root
```

The toolchain is pinned in `mise.toml` (`mise install` in this directory). `-race` needs cgo, so `bin/test`
uses the pinned zig as the C compiler. See [`test-service/README.md`](test-service/README.md) for how the
conformance service maps the protocol onto this SDK, and [`RUN.md`](RUN.md) for the run that wrote it.
