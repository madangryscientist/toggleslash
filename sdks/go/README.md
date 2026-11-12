# ToggleSlash Go server SDK

A server SDK for [ToggleSlash](../../README.md) in Go. It holds one environment's ruleset in memory, evaluates
flags locally to [`docs/evaluation.md`](../../docs/evaluation.md), and reports usage counts. It is a server SDK:
the key is a server key (`fs_server_...`), never a client key.

Nothing is published: the module is used from a checkout of this repository.

## Quick start

```sh
git clone https://github.com/madangryscientist/toggleslash
cd toggleslash/sdks/go
mise trust && mise install
```

```go
package main

import (
	"fmt"
	"os"

	"github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	client := toggleslash.New(toggleslash.Options{
		URL:    "http://127.0.0.1:8080",
		SDKKey: os.Getenv("TOGGLESLASH_SDK_KEY"),
		OnError: func(err error) {
			fmt.Println("toggle slash:", err)
		},
	})
	defer client.Close()

	client.Ready()

	context := toggleslash.Context{"key": "user-1", "attributes": map[string]any{"plan": "pro"}}
	if client.BoolVariation("new-checkout", context, false) {
		fmt.Println("the new checkout is on for user-1")
	}
	port := client.Int64Variation("port", context, 8080)
	fmt.Println("port:", port)
}
```

To point a module at the checkout without a tag, add a `replace` to the consuming `go.mod`:

```
require github.com/madangryscientist/toggleslash/sdks/go v0.0.0
replace github.com/madangryscientist/toggleslash/sdks/go => /path/to/toggleslash/sdks/go
```

## The API in one table

[`DESIGN.md`](DESIGN.md#1-the-mapping-table-contract-2) holds the mapping table: every canonical operation and
option (contract section 2) and what it is called here. In short:

| Contract | Go |
| --- | --- |
| `create` | `toggleslash.New(toggleslash.Options{...})` |
| `ready` | `client.Ready()` |
| `variation` | `client.Variation`, `BoolVariation`, `StringVariation`, `Float64Variation`, `Int64Variation` |
| `variationDetail` | the same with a `Detail` suffix |
| `status` | `client.Status()` |
| `close` | `client.Close()` |

The closed sets keep the contract's spelling: `ReasonRuleMatch`, `CodeTypeMismatch`, `StreamConnected`,
`LastErrorTimeout`, and `LastError("http_429")` for a status.

## Guarantees

Contract section 5, in short:

1. It never panics at a caller. A failure becomes the fallback with an `ERROR` reason, goes to `OnError`, or both.
2. `Variation`, the typed accessors and `Status` never do I/O and never wait: they read memory and run the
   evaluator, and no lock a request holds is taken.
3. `Ready` settles after the first attempt, bounded by the request timeout, and its answer never changes.
4. While the server is unreachable the SDK keeps serving the last ruleset it held.
5. Only a ruleset replaces a ruleset.
6. `Close` is safe twice, from many goroutines, and returns within about twice the request timeout.

Nothing about an end user leaves the process: the only requests are the ruleset, the stream and the usage counts,
and no context is in any of them.

## Checks

```sh
(cd sdks/go && mise trust && mise install)   # the toolchain mise.toml pins
sdks/go/bin/lint                             # go vet, golangci-lint, gofmt -l
sdks/go/bin/test                             # builds, then every test under the race detector
bun run conformance -- --service sdks/go/bin/test-service   # from the repository root
```

`bin/test` also prints `vectors: <n> pins: <n>`, the count of the shared evaluation vectors and bucket pins it
ran. The test service is the SDK wrapped in the protocol of
[`docs/sdk-test-service.md`](../../docs/sdk-test-service.md); see [`test-service/README.md`](test-service/README.md).

`bin/test` needs `go`, `golangci-lint` is for `bin/lint`, and the race detector needs a C compiler: `mise.toml`
pins `zig` and `bin/test` names it by path, so a `zig` elsewhere on `PATH` cannot shadow the pinned one.
