# The Java SDK's test service

`src/com/github/madangryscientist/toggleslash/sdks/java/service/Main.java` is the conformance test service
for `docs/sdk-test-service.md`. It is a test source root of the pom (build-helper-maven-plugin), so
`bin/test` compiles it; `bin/test-service` runs the compiled classes and builds only if they are missing.

It uses `com.sun.net.httpserver.HttpServer` on `127.0.0.1` with a system-picked port, prints
`listening http://127.0.0.1:<port>` and serves requests concurrently on daemon threads. It catches nothing
the SDK throws: a call that raises answers 500 with the stack trace, and a typed accessor given a fallback
of another kind answers 400. It advertises the `integer-accessor` capability.

## Accessor mapping

| Protocol accessor | Fallback it needs | SDK method |
| --- | --- | --- |
| `bool` | boolean | `boolVariationDetail` |
| `string` | string | `stringVariationDetail` |
| `number` | number | `floatVariationDetail` |
| `int` | number | `intVariationDetail` (the fallback is read as a `long`) |
| `json` | anything, null included | `variationDetail` |
| `any` | anything, null included | `variationDetail` |

The service writes JSON with `JsonOut`, which writes a non-finite number as `null`, as the protocol says.
The `hook` option records every `EvaluationEvent` for `GET .../hook-events`; `hookThrows` and `onErrorThrows`
make those callbacks raise so the harness can check the SDK survives it.
