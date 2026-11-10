# The Java SDK's test service

`src/.../TestService.java` wraps the SDK in the protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the SDK
from outside. It is a plain `com.sun.net.httpserver.HttpServer` on `127.0.0.1` and a port the OS picks, with
no dependency beyond the SDK and the JDK:

```bash
sdks/java/bin/test    # builds everything, the test service included, as part of `mvn clean test`
bun run conformance -- --service "sdks/java/bin/test-service"
```

It advertises `integer-accessor`: Java has `long` next to `double`, so the `int` accessor is real.

| Protocol | Java SDK |
| --- | --- |
| `POST /clients` | `Client.create(Options.builder(url, sdkKey)...build())`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` become `stream(boolean)`, `pollInterval(Duration)`, `flushInterval(Duration)` and `requestTimeout(Duration)`; `onError` always records, and throws after recording when `onErrorThrows` is set |
| `ready` | `client.ready()` |
| `evaluate`, accessor `bool` | `client.boolVariationDetail(key, context, fallback)` |
| `evaluate`, accessor `string` | `client.stringVariationDetail(key, context, fallback)` |
| `evaluate`, accessor `number` | `client.doubleVariationDetail(key, context, fallback)` |
| `evaluate`, accessor `int` | `client.intVariationDetail(key, context, fallback)`, the JSON fallback number taken as a `long` |
| `evaluate`, accessors `json` and `any` | `client.jsonVariationDetail(key, context, fallback)`, the fallback passed through as the JSON value it arrived as |
| `status` | `client.status()`, written as the four keys of the protocol (`lastSyncAt` as epoch milliseconds) |
| `hook-events` | the events `Options.Builder.afterEvaluation` received (installed when `hook` or `hookThrows` is set; `hookThrows` throws after recording) |
| `errors` | what `Options.Builder.onError` received, each as its message |
| `close` | `client.close()` |

The context goes in as raw JSON (`Context.fromJson`), valid or not: an invalid one is a case the SDK answers
with `ERROR INVALID_CONTEXT`. A call that fails or throws answers 500 with its text, and the harness fails the
scenario. The service serves concurrently (a cached thread pool) and keeps a client answerable after `close`.

`bin/test-service` starts the built classes (`java -cp target/classes:target/test-classes`) and builds them
only if they are missing, because the harness waits 30 s for the `listening` line.
