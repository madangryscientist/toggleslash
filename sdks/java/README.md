# ToggleSlash Java server SDK

The server SDK for Java 17 and later. It evaluates flags locally, with a port of the evaluator in
[`docs/evaluation.md`](../../docs/evaluation.md), against a ruleset it keeps in step with a ToggleSlash server
(the stream, with a polling fallback), and reports usage counts. It has no runtime dependency beyond the JDK.
The design, with the full mapping table, is in [`DESIGN.md`](DESIGN.md); the rules it keeps are
[`docs/sdk-contract.md`](../../docs/sdk-contract.md). Nothing is published: build it from a checkout of this
repository.

## Quickstart

```sh
(cd sdks/java && mise trust && mise install)
(cd sdks/java && mise exec -- mvn -B -ntp install)   # puts com.github.madangryscientist.toggleslash:toggleslash-sdk-java in your local Maven repository
```

```java
import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import java.util.Map;

try (Client client = Client.create(Options.builder("http://localhost:5080", System.getenv("TOGGLESLASH_SDK_KEY")).build())) {
  client.ready(); // waits for the first load, bounded by the request timeout
  Map<String, Object> context = Map.of("kind", "user", "key", "user-42");
  boolean on = client.boolVariation("new-checkout", context, false);
}
```

## The mapping

| Canonical | In Java |
| --- | --- |
| `create`, `ready`, `status`, `close` | `Client.create(Options)`, `ready()`, `status()`, `close()` |
| `variation` | `boolVariation`, `stringVariation`, `floatVariation`, `intVariation`, and `variation` for any kind |
| `variationDetail` | the same names ending in `Detail`, each returning `Detail<T>(value, variationIndex, reason)` |
| options | `Options.builder(url, sdkKey)` with `stream`, `pollInterval`, `flushInterval`, `requestTimeout`, `afterEvaluation`, `onError` |

See [`DESIGN.md`](DESIGN.md) for every row, with defaults.

## Guarantees

- No call throws at the caller: a failure is a fallback with an `ERROR` reason, and `onError` hears of it.
- `variation` and `status` do no I/O and wait on no network call.
- `ready` is fixed by the first attempt and answers the same afterwards.
- The last good ruleset is kept; a ruleset that is not valid never replaces it.
- No request, status or error message carries a context or an end-user value.
- The SDK's threads are daemon threads; `close` stops them and sends the last usage counts, bounded by the request timeout.

## Checks

From the repository root, with the toolchain installed as above and `bun install` run once:

```sh
sdks/java/bin/lint     # Spotless (google-java-format) over src and test-service, Checkstyle over src/main
sdks/java/bin/test     # clean build, every JUnit test, the evaluation vectors included
bun run conformance -- --service sdks/java/bin/test-service
```

`bin/test` runs a TLS test, so it needs `keytool`, which the pinned JDK has.
