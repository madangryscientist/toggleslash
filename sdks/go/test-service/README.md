# Go test service

The conformance test service for the Go SDK, to the protocol in
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md). It is `package main` in this directory,
one more package of the same module, so `go vet ./...` and `go build ./...` compile it with the SDK and
`bin/test` builds it last, into `.build/test-service`. `bin/test-service` runs that binary and builds it
only when it is missing.

It listens on `127.0.0.1`, port 0, and prints `listening http://127.0.0.1:<port>` on stdout (unbuffered in
Go). It serves each request in its own goroutine, so a `ready` that waits does not hold up the others, and a
panic that reaches a handler is answered 500 with its text. It advertises `integer-accessor`.

## Accessor mapping

| `accessor` | SDK method | Fallback it takes |
| --- | --- | --- |
| `bool` | `BoolVariationDetail` | a JSON boolean, else 400 |
| `string` | `StringVariationDetail` | a JSON string, else 400 |
| `number` | `FloatVariationDetail` | a JSON number, else 400 |
| `int` | `IntVariationDetail` (`int64`) | a whole JSON number inside the `int64` range, else 400 |
| `json`, `any` | `JSONVariationDetail` | any JSON value, `null` included |

The context is passed through as the JSON value it arrived as (`ContextOf`). A number that is not finite is
written back as `null`. The reason is written with only the fields its kind has (`ruleIndex`, `flagKey`,
`code`), and a variation index of `-1` is written as `null`.

## Options and hooks

`stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `WithStream`,
`WithPollInterval`, `WithFlushInterval` and `WithRequestTimeout`. `hook` installs `WithAfterEvaluation`, which
records each event; `hookThrows` records and then panics, and `onErrorThrows` makes the always-installed
`WithOnError` record and then panic. The SDK recovers both. A status is written with `etag` and `lastError` as
`null` when empty, and `lastSyncAt` as whole epoch milliseconds or `null`.
