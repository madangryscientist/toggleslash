# ToggleSlash server SDK for Java

The Java server SDK for ToggleSlash. It evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md), over a ruleset it keeps current from the server, and keeps the
contract in [`docs/sdk-contract.md`](../../docs/sdk-contract.md). Maven coordinates
`com.github.madangryscientist.toggleslash:toggleslash-sdk-java`, package
`com.github.madangryscientist.toggleslash.sdks.java`. It needs Java 17 or later and has no dependency beyond the
JDK. Nothing is published and no registry is named: build it from a checkout of this repository.

## Quickstart

From a checkout, with no tag, install it into the local Maven repository and depend on it:

```sh
(cd sdks/java && mise trust && mise install)
(cd sdks/java && mise exec -- mvn --batch-mode -DskipTests install)
```

```xml
<dependency>
  <groupId>com.github.madangryscientist.toggleslash</groupId>
  <artifactId>toggleslash-sdk-java</artifactId>
  <version>0.0.0-SNAPSHOT</version>
</dependency>
```

```java
import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Context;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import java.util.Map;

public final class Example {
  public static void main(String[] args) {
    try (Client client =
        Client.create(
            Options.builder()
                .url("https://flags.example.test")
                .sdkKey(System.getenv("TOGGLESLASH_SDK_KEY"))
                .build())) {
      client.ready(); // optional: waits for the first load, bounded by the request timeout

      Object user = Context.of("u1", Map.of("plan", "pro"));
      if (client.boolVariation("new-checkout", user, false)) {
        System.out.println("new checkout");
      }
      Detail<String> detail = client.stringVariationDetail("banner", user, "none");
      System.out.println(detail.value() + " " + detail.variationIndex() + " " + detail.reason().kind());
    }
  }
}
```

`Client.create` returns at once. Before the first load, and when a flag is missing, malformed or of another type,
an accessor answers the fallback, and the `Detail` form says why. A context is a `Map<String, Object>` (or a
list of such maps for a multi-kind context); `Context.of` builds the common shapes.

## The mapping

The canonical names of the contract map to Java in the table in [`DESIGN.md`](DESIGN.md#1-the-mapping-table-contract-section-2),
with the closed sets (reason kinds, error codes, stream states, `lastError` codes) and their spelling.
`DESIGN.md` also holds the concurrency model, the HTTP and stream handling, the dependencies (none beyond the JDK)
and the package layout.

## Guarantees

From contract section 5:

- No operation throws at the caller, whatever the server, the network or the context does.
- An evaluation never waits on the network: it reads the ruleset held, or answers `NOT_READY` before the first load.
- `ready()` settles after the first attempt, bounded by the request timeout, and may be called again.
- An answer that is not a ruleset never replaces the one held, and a ruleset and its ETag are swapped together.
- Every operation is safe from many threads at once, and `close()` is safe to call twice or concurrently.
- A callback that throws, or calls back into the client, neither breaks the client nor deadlocks it.
- No context, context key or attribute appears in a request, an error or a log line; the SDK logs nothing.
- Usage counts are batched, kept through a failure the server may recover from, and flushed once more on `close()`.
- The SDK's own threads (`toggleslash-sync`, `toggleslash-flush`) are daemon threads, so a program that forgets
  `close()` can still exit.

## Toolchain and checks

```sh
(cd sdks/java && mise trust && mise install)   # Temurin, Maven, Checkstyle and google-java-format, as sdks/java/mise.toml pins them

sdks/java/bin/lint            # google-java-format (check only) and Checkstyle, no warnings allowed
sdks/java/bin/test            # builds, then native tests, every evaluation vector, and the test service build
bun run conformance -- --service sdks/java/bin/test-service    # from the repository root, after bun install
```

Java has no race detector, so the many-thread test in `src/test/java/.../StressTest.java` stands in for it: it
calls every operation from twelve threads while the ruleset changes and two threads race `close()`, and fails on
any exception, torn snapshot, hang or lost count. `sdks/java/test-service` is the conformance test service; see its
[`README.md`](test-service/README.md) for the accessor mapping.
