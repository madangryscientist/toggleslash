# The Java SDK's test service

`src/com/github/madangryscientist/toggleslash/sdks/java/testservice/Main.java` wraps `Client` in the protocol of
[`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md), so the conformance harness can drive the SDK
from outside. It uses the JDK's own `com.sun.net.httpserver` on `127.0.0.1`, a port the OS picks, and prints
`listening http://127.0.0.1:<port>` once it is bound.

`mvn test` compiles it with the tests (`build-helper-maven-plugin` adds `test-service/src` as a test source
root). `bin/test-service` runs the compiled classes and builds only when they are missing:

```sh
bun run conformance -- --service sdks/java/bin/test-service
```

It advertises one capability, `integer-accessor`: Java has an integer type distinct from its floating type.

| Protocol | Java SDK |
| --- | --- |
| `POST /clients` | `Client.create(Options)`; `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` pass through (a millisecond count becomes a `Duration`), `onError` always records |
| `ready` | `client.ready()` |
| `evaluate`, accessor `bool` | `client.boolVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `string` | `client.stringVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `number` | `client.floatVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `int` | `client.intVariationDetail(flagKey, context, fallback)`, the fallback as a `long` |
| `evaluate`, accessor `json` | `client.jsonVariationDetail(flagKey, context, fallback)` |
| `evaluate`, accessor `any` | `client.variationDetail(flagKey, context, fallback)`, the generic method; a `null` fallback accepts any served kind |
| `status` | `client.status()` |
| `hook-events` | the `EvaluationEvent`s `afterEvaluation` received, as the SDK built them |
| `errors` | what `onError` received, each as its message |
| `close` | `client.close()`; the client stays answerable afterwards |
| `callback-close` | `closeInHook` and `closeInOnError`: the first callback that finds the client stored calls `client.close()`, times it and catches anything it throws |

The `reason` in an answer is `{kind, ruleIndex?, flagKey?, code?}`: `code` is the Java `errorCode`, spelled as its
enum constant. The context is passed through as the JSON it arrived as (a `Map`), so the SDK, not the service,
answers an invalid one.

The service catches nothing the SDK throws except to answer 500 with the error text, and a request that raises
anywhere answers 500 too rather than dropping the connection.
