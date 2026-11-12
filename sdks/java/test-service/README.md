# The Java SDK's test service

`src/com/github/madangryscientist/toggleslash/sdks/java/testservice/Main.java` wraps `Client` in the protocol
of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the
SDK from outside:

```bash
bun run conformance -- --service "sdks/java/bin/test-service"
```

`bin/test-service` builds the classes with Maven when they are missing or older than a source (the build
output goes to stderr), then runs `java -cp target/classes:target/test-classes ...testservice.Main`. The
service uses the JDK's own `com.sun.net.httpserver`, so it adds no dependency, and it is compiled as a test
source root (`build-helper-maven-plugin` in `pom.xml`), so it never reaches the SDK's jar.

It advertises one capability, `integer-accessor`: Java has `long` and `Client.longVariation`.

| Protocol | Java SDK |
| --- | --- |
| `POST /clients` | `Client.create(Options)`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` pass through (as `Duration.ofMillis`), `onError` always records the throwable's message |
| `ready` | `client.ready()` |
| `evaluate`, accessor `bool` | `client.boolVariationDetail(flagKey, context, (Boolean) fallback)` |
| `evaluate`, accessor `string` | `client.stringVariationDetail(...)` |
| `evaluate`, accessor `number` | `client.numberVariationDetail(...)` with the fallback as a `double` |
| `evaluate`, accessor `int` | `client.longVariationDetail(...)` with the fallback as a `long` (a fallback that is not whole answers 400) |
| `evaluate`, accessors `json` and `any` | `client.jsonVariationDetail(flagKey, context, fallback)`: the fallback may be null or of any kind |
| `status` | `client.status()`; `lastSyncAt` as epoch milliseconds, `stream` as `StreamState.wire()` |
| `hook-events` | the `EvaluationEvent`s `Options.afterEvaluation` received, with `reason` mapped to `{ kind, code, ruleIndex, flagKey }` and `context` the JSON the harness sent |
| `errors` | what `onError` received, each as its message |
| `close` | `client.close()` |

The typed accessors (`bool`, `string`, `number`, `int`) answer 400 to a fallback that is null or of another
kind, which the protocol allows. A number that is not finite in a result is written as JSON `null`.

A context that is a JSON object goes through `Context.of`; anything else (null, a string, an array) becomes an
invalid context, so the SDK answers `ERROR INVALID_CONTEXT` as the scenarios expect.

It catches nothing the SDK throws: a call that raises answers 500 with the stack trace.
