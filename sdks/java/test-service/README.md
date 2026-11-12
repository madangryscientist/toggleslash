# The Java SDK's test service

`test-service/src/com/github/madangryscientist/toggleslash/sdks/java/service/Main.java` wraps `ToggleSlashClient` in the
protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can
drive the SDK from outside. It is a **test source root**: `pom.xml` adds `test-service/src` with
`build-helper-maven-plugin`, so `bin/test` compiles it with the tests, and `bin/test-service` runs the
compiled classes (`java -cp target/classes:target/test-classes ...service.Main`), building only when they are
missing. It is in the `com.github.madangryscientist.toggleslash.sdks.java.service` package, so it reaches
only the SDK's public API.

```bash
bin/test && bun run conformance -- --service sdks/java/bin/test-service
```

It listens on `127.0.0.1` with an OS-picked port (the JDK's `com.sun.net.httpserver`, a cached pool of daemon
threads, so scenarios run concurrently) and prints `listening http://127.0.0.1:<port>`, flushed.

It advertises one capability, `integer-accessor`: Java's `long` is an integer type apart from `double`, and
`intVariation` answers `TYPE_MISMATCH` for a fraction or a value outside signed 64 bits.

| Protocol | Java SDK |
| --- | --- |
| `POST /clients` | `ToggleSlashClient.create(Options.builder(url, sdkKey)...build())`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` pass through as `Duration`s; `onError` always records |
| `ready` | `client.ready()` |
| `evaluate`, accessor `bool` | `client.boolVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `string` | `client.stringVariationDetail(...)` |
| `evaluate`, accessor `number` | `client.doubleVariationDetail(...)` |
| `evaluate`, accessor `int` | `client.intVariationDetail(...)` (the fallback must be an integral JSON number) |
| `evaluate`, accessors `json` and `any` | `client.variationDetail(flagKey, context, fallback)`: the generic method; the fallback's kind does the work, and `null` accepts any kind |
| `status` | `client.status()`; `lastSyncAt` as whole epoch milliseconds or `null` |
| `hook-events` | the `EvaluationEvent`s `afterEvaluation` received (switch `hook`) |
| `errors` | what `onError` received, each as its message |
| `close` | `client.close()` |

The context passes through as the JSON it arrived as, parsed by `Json.parse`. The switches `hook`,
`hookThrows` and `onErrorThrows` install a recording hook, a hook that throws and an `onError` that throws.
The service catches nothing the SDK throws: a call that raises answers 500 with the error text. A typed
accessor given a fallback of the wrong kind answers 400 (the protocol's own error, not the SDK's). A client
stays answerable after `close`.
