# The Java SDK's test service

`src/testservice/Main.java` wraps `Client.create` in the protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the SDK
from outside. It is a plain `com.sun.net.httpserver` server on `127.0.0.1` and a port the OS picks, and it
prints `listening http://127.0.0.1:<port>` when it is ready.

**Where it lives in the build.** It is part of the test build, not of the SDK's jar: `pom.xml` adds
`test-service/src` as a test source root (build-helper), so `bin/test` compiles it into `target/test-classes`
with the SDK. `bin/test-service` runs it from there and builds only when the classes are missing:

```bash
bun run conformance -- --service sdks/java/bin/test-service
```

It advertises one capability, `integer-accessor`.

| Protocol | Java SDK |
| --- | --- |
| `POST /clients` | `Client.create(Options)`; `pollIntervalMs`, `flushIntervalMs`, `requestTimeoutMs` and `stream` pass through the builder, `onError` always records (and throws when `onErrorThrows` is set), the hook is installed only when `hook` or `hookThrows` is set |
| `ready` | `client.ready()` |
| `evaluate`, accessor `bool` | `client.boolVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `string` | `client.stringVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `number` | `client.doubleVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `int` | `client.longVariationDetail(flagKey, context, fallback)`: the signed 64-bit integer accessor |
| `evaluate`, accessors `json` and `any` | `client.variationDetail(flagKey, context, fallback)`: the generic method, where the fallback's kind decides and a `null` fallback takes any JSON value |
| `status` | `client.status()` |
| `hook-events` | the `EvaluationEvent`s `hooks.afterEvaluation` received, as the SDK built them |
| `errors` | what `onError` received, each as its message |
| `close` | `client.close()` |

It catches nothing the SDK throws: a call that raises answers 500 with the error text.
