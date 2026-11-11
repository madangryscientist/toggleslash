# ToggleSlash Go SDK

A server SDK for ToggleSlash in Go. It holds one **server** key, keeps its environment's ruleset in memory,
evaluates flags locally to [`docs/evaluation.md`](../../docs/evaluation.md), and reports usage counts. The
contract it keeps is [`docs/sdk-contract.md`](../../docs/sdk-contract.md). It is not a client-side SDK, and it is
not published: no registry holds it, and the module path is only a name.

It needs Go 1.26 or later and has no dependency outside the standard library. It reads JSON with its own
reader, evaluates patterns with its own matcher (no `regexp`) and reads no environment variable.

## Quickstart

Use it from a checkout of this repository, with a `replace` directive in your own module (no tag exists):

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
	client := toggleslash.NewClient("https://flags.example.test", "fs_server_...",
		toggleslash.WithOnError(func(err error) { fmt.Println("toggleslash:", err) }))
	defer client.Close()

	if !client.Ready() {
		fmt.Println("no ruleset yet: every answer is the fallback until one loads")
	}

	ctx := toggleslash.UserContext("user-42").With("plan", toggleslash.StringValue("pro"))
	if client.BoolVariation("new-checkout", ctx, false) {
		fmt.Println("new checkout")
	}

	detail := client.StringVariationDetail("banner", ctx, "none")
	fmt.Println(detail.Value, detail.Reason.Kind)
}
```

`NewClient` never fails and does not wait for the network: until the first ruleset loads, every evaluation is
the fallback with the reason `ERROR NOT_READY`. `Close` sends the last usage counts. The domain above is a
placeholder: the product has no address yet.

## The API

[`DESIGN.md`](DESIGN.md) section 1 is the mapping table from the contract's names to Go's, option by option.

| Contract | Go |
| --- | --- |
| `create` | `NewClient(url, sdkKey, opts...)` |
| `ready` | `(*Client).Ready() bool` |
| `variation` | `BoolVariation`, `StringVariation`, `IntVariation` (`int64`), `FloatVariation`, `JSONVariation`, `AnyVariation` |
| `variationDetail` | the `...VariationDetail` twins, returning `Detail[T]{Value, VariationIndex, Reason}` |
| `status` | `(*Client).Status() Status` |
| `close` | `(*Client).Close()` |
| options | `WithStream`, `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout`, `WithAfterEvaluation`, `WithOnError`, `WithHTTPClient` |

Contexts are built with `UserContext`, `NewContext`, `MultiContext`, `ContextFromJSON`, `ContextFromMap` or
`ContextFromValue`. A context that cannot be built is invalid and evaluates to `ERROR INVALID_CONTEXT`.

## Guarantees

The short form of contract section 5:

- It never panics or returns an error to a caller. A failure is the fallback with an `ERROR` reason, or a call
  to `OnError`, or both.
- `Variation`, its `Detail` twins and `Status` do no I/O and never wait.
- `Ready` settles after the first attempt, is bounded by the request timeout, and its answer is fixed then.
- While the server is out of reach it keeps evaluating from the last good ruleset.
- Only a JSON object whose `flags` and `segments` are both arrays replaces a ruleset.
- Every method is safe to call from many goroutines. `Close` is idempotent, safe to call concurrently and
  returns within about twice the request timeout. `Ready` and `Close` called from an SDK callback do not wait.
- An unclosed client does not keep the process from exiting.
- It sends the key, `Accept`, `If-None-Match` and `Content-Type`, and nothing about an end user; counts carry no
  context. The headers `net/http` adds itself are listed in [`RUN.md`](RUN.md).
- A redirect is an `http_3xx` error and is never followed, so `Authorization` is never sent to a redirect target
  (unless you pass your own client with `WithHTTPClient`).

## Develop

The toolchain is pinned in [`mise.toml`](mise.toml): Go, golangci-lint and zig. From this directory, or
through the scripts from anywhere:

```sh
bin/lint          # go vet, golangci-lint (.golangci.yml) and gofmt -l, no warnings allowed
bin/test          # builds, runs every test under -race, prints "vectors: <n> pins: <n>", builds the test service
```

**`-race` needs cgo, so `bin/test` needs a C compiler.** It uses `gcc` or `cc` when the machine has one and
`zig cc` otherwise (the CI workflow installs zig for that reason). Without any of them the race detector cannot
build.

The conformance harness drives the SDK through its test service ([`test-service/`](test-service/README.md))
from the repository root:

```sh
bun run conformance -- --service sdks/go/bin/test-service
```
