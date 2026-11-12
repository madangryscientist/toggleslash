# The Java SDK's test service

`src/com/github/madangryscientist/toggleslash/sdks/java/testservice/Main.java` wraps `ToggleSlashClient` in the
protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the
SDK from outside.

It lives in `test-service/src`, which `build-helper-maven-plugin` adds to the Maven test source roots (see
`pom.xml`), so `mvn test` compiles it with the tests and `bin/test` leaves its classes in `target/test-classes`.
`bin/test-service` runs `java -cp target/classes:target/test-classes ...testservice.Main` and builds only when
those directories are missing, so the harness's 30 s wait for the `listening` line never includes a build.

```bash
bin/test && bun run conformance -- --service sdks/java/bin/test-service    # from the repository root
```

It advertises `integer-accessor`: Java has `long` distinct from `double`, and `intVariation` applies the
fractional-part and signed 64-bit range rules of contract section 4.

| Protocol | Java SDK |
| --- | --- |
| `POST /clients` | `ClientOptions.builder(url, sdkKey)`, then `ToggleSlashClient.create(options)`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` pass through as `Duration`s, `onError` always records |
| `ready` | `client.ready()` |
| `evaluate`, accessor `bool` | `client.boolVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `string` | `client.stringVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `number` | `client.doubleVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `int` | `client.intVariationDetail(flagKey, context, fallback)` (fallback read as a `long`) |
| `evaluate`, accessors `json` and `any` | `client.jsonVariationDetail(flagKey, context, fallback)`: any JSON fallback, `null` included |
| `status` | `client.status()`: `etag`, `lastSyncAt` as epoch milliseconds, `stream` as `Status.stream().wire()`, `lastError` |
| `hook-events` | the `EvaluationEvent`s that `afterEvaluation` received, with `reason` written as `{ kind, ruleIndex?, flagKey?, code? }` |
| `errors` | what `onError` received, each as its message |
| `close` | `client.close()` |

The context is passed through as the parsed JSON (`Map`, `List`, `String`, `Double`, `Boolean`, `null`); the SDK, not
the service, answers an invalid one. The typed accessors cast the fallback and so answer 500 to a fallback of
another kind, which the protocol allows. A call that raises answers 500 with the error text.
