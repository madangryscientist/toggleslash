# The Go SDK's conformance test service

`main.go` wraps the SDK in the HTTP server that `docs/sdk-test-service.md` describes, so
`packages/sdk-conformance` can drive it from outside. It listens on `127.0.0.1:0` and prints
`listening http://127.0.0.1:<port>` as its first line of output.

## Running it

`bin/test-service` is the command the harness starts. It runs under the pinned toolchain (`mise exec`),
builds `bin/.test-service` when that file is missing, and runs it. `bin/test` always rebuilds
`bin/.test-service`, which is where the service is compiled for the conformance run; delete the file to force
a rebuild by hand. From the repository root:

```sh
sdks/go/bin/test && bun run conformance -- --service sdks/go/bin/test-service
```

## Capabilities

`GET /` answers `{"name": "toggleslash-sdk (Go)", "capabilities": ["integer-accessor"]}`. The Go SDK has a
true integer accessor (`IntVariationDetail`, int64), so it advertises `integer-accessor` and the harness
runs the integer-range scenarios against it.

## Accessor mapping

The protocol's `accessor` field picks the SDK method. The fallback must have the accessor's type, else the
service answers 400.

| `accessor` | SDK method | Fallback in the request |
| --- | --- | --- |
| `bool` | `BoolVariationDetail` | a boolean |
| `string` | `StringVariationDetail` | a string |
| `number` | `FloatVariationDetail` | a number |
| `int` | `IntVariationDetail` | a whole number inside the int64 range |
| `json` | `JSONVariationDetail` | any JSON value (null accepts any served value) |
| `any` (or absent) | `JSONVariationDetail` | any JSON value |

A number the service would write that is not finite (`1e999` served to the `number` accessor) is written as
`null`, as `JSON.stringify` does. Anything the SDK raises reaches the service's recover, which answers 500
with the text, and the harness then fails the scenario.

## Options

`POST /clients` maps `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` onto the SDK's
options. `hook` and `hookThrows` install an `afterEvaluation` function that records each event (and, for
`hookThrows`, panics); `onErrorThrows` makes the `onError` function record its message and panic. The
recorded events and messages come back from `GET /clients/<id>/hook-events` and `/errors`.
