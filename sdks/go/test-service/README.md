# Go SDK conformance test service

`main.go` wraps `toggleslash.New` in the HTTP protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md),
so `packages/sdk-conformance` can drive the SDK from outside. From the root of the repository:

```sh
bun run conformance -- --service sdks/go/bin/test-service
```

`bin/test-service` runs `.build/test-service` (built by `bin/test`, or on first use) and the service prints
`listening http://127.0.0.1:<port>` once it listens. It binds loopback only and sets no timeout, so `ready` and
`close` wait as long as the SDK takes.

It advertises one capability, `integer-accessor`.

## Accessor mapping

| `accessor` | SDK call | Fallback it needs |
| --- | --- | --- |
| `bool` | `BoolVariationDetail` | a boolean |
| `string` | `StringVariationDetail` | a string |
| `number` | `FloatVariationDetail` | a number |
| `int` | `IntVariationDetail` (int64) | a number with no fraction |
| `json` | `JSONVariationDetail` | any JSON value, null included |
| `any` (the default) | `JSONVariationDetail` | any JSON value, null included |

A typed accessor given a fallback of another kind answers 400, which the protocol allows. The context goes to
the SDK as the parsed JSON value, valid or not, so an invalid context reaches the SDK and must come back as
`ERROR INVALID_CONTEXT`. A served number that is not finite (`1e999`) is written back as `null`.

## Hooks and errors

`hook` installs an `afterEvaluation` hook that records each event; `hookThrows` records it and then panics;
`onErrorThrows` makes the recording `onError` panic after it records. The SDK recovers each panic. Anything else
the SDK raises is not absorbed: the handler answers 500 with the panic text. An unknown client or route answers 404.
