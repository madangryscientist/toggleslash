# Java test service

The conformance test service for the Java SDK, to `docs/sdk-test-service.md`. It is one class,
`test-service/src/com/github/madangryscientist/toggleslash/sdks/java/service/Main.java`, on the JDK's
`com.sun.net.httpserver.HttpServer`, and it uses only the SDK's public API. It compiles with the tests
(`pom.xml` adds `test-service/src` as a test source root).

Run it: `bin/test-service` (it builds `target/classes` and `target/test-classes` first when they are
missing). From the repository root:

```
bun run conformance -- --service "sdks/java/bin/test-service"
```

## Accessors

| `accessor` | SDK method | Fallback |
| --- | --- | --- |
| `bool` | `booleanVariationDetail` | the JSON boolean |
| `string` | `stringVariationDetail` | the JSON string, or null |
| `number` | `doubleVariationDetail` | the JSON number as a double |
| `int` | `longVariationDetail` | the JSON number as a long (the integer accessor) |
| `json` | `jsonVariationDetail` | any JSON value, null included |
| `any` | `variationDetail` (the generic method; the fallback's class picks the rules) | any JSON value, null included |

Capabilities: `integer-accessor`.

A typed accessor given a null or another-kind fallback is not sent by any scenario; the service lets
the cast raise, which answers 500.

## Options and hooks

`hook` installs an `afterEvaluation` hook that records each event; `hookThrows` records and then throws;
`onErrorThrows` makes the recording `onError` throw after recording. The hook event's `value` is the
boxed value the caller receives: a `Long` for the integer accessor, a `Double` for the number accessor.
