# Go SDK test service

`main.go` wraps the SDK for the conformance harness in `packages/sdk-conformance`. The protocol is
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md). Run it through `bin/test-service`, which builds
`.build/test-service` once and starts it; it prints `listening http://127.0.0.1:<port>`.

## Accessors

Each name the harness sends in `accessor` calls one method of `*toggleslash.Client`, with the fallback the
harness gave. The service catches nothing the SDK raises: a panic answers 500 with the text, and the scenario
fails.

| Accessor | SDK method |
| --- | --- |
| `bool` | `BoolVariationDetail` |
| `string` | `StringVariationDetail` |
| `number` | `FloatVariationDetail` |
| `int` | `IntVariationDetail` (capability `integer-accessor`) |
| `json` | `JSONVariationDetail` |
| `any` | `JSONVariationDetail` (the generic method; the harness sends a null fallback, which accepts any served type, and the default when `accessor` is missing) |

Capabilities advertised at `GET /`: `integer-accessor`.
