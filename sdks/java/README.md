# ToggleSlash server SDK for Java

The Java server SDK: it keeps one environment's ruleset current and evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md) and [`docs/sdk-contract.md`](../../docs/sdk-contract.md). It
needs Java 17 or newer, has no runtime dependency, and is private and unpublished: no registry carries it.

## Quickstart

From a checkout, with no tag, compile the SDK and put its classes on your class path:

```sh
(cd sdks/java && mise trust && mise install)      # the toolchain, once
mvn -q -f sdks/java/pom.xml compile
javac -cp sdks/java/target/classes Shop.java
java -cp sdks/java/target/classes:. Shop
```

```java
import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Context;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import java.util.Map;

public final class Shop {
  public static void main(String[] args) {
    Options options =
        Options.builder()
            .url("https://flags.example.test")
            .sdkKey(System.getenv("TOGGLESLASH_SDK_KEY"))
            .build();
    try (Client client = Client.create(options)) {
      client.ready(); // optional: waits for the first load, bounded by the request timeout

      Context user = Context.user("u1", Map.of("plan", "pro"));
      if (client.boolVariation("new-checkout", user, false)) {
        System.out.println("new checkout");
      }

      Detail<String> banner = client.stringVariationDetail("banner", user, "none");
      System.out.println(banner.value() + " " + banner.variationIndex() + " " + banner.reason().kind());
    }
  }
}
```

`Client.create` returns at once and starts two daemon threads. Before the first load, and when a flag is missing,
malformed or of another type, an accessor answers the fallback, and the `Detail` form says why. The accessors are
`boolVariation`, `stringVariation`, `longVariation`, `numberVariation` and `jsonVariation`, each with a `Detail`
twin; [`DESIGN.md`](DESIGN.md) holds the full mapping table of the contract's names onto Java.

## Guarantees

From contract section 5:

- No operation throws at the caller, whatever the server, the network or the context does.
- An evaluation never waits on the network: it reads the ruleset held, or answers `NOT_READY` before the first load.
- `ready` settles after the first attempt, bounded, and may be called again with the same answer.
- An answer that is not a ruleset never replaces the one held, and a ruleset and its ETag are swapped together.
- Every operation is safe from many threads at once, and `close` is safe to call twice or concurrently.
- A callback that throws, or calls back into the client, neither breaks the client nor deadlocks it.
- No context, context key or attribute appears in a request, an error or a log line.
- Usage counts are batched, kept through a failure the server may recover from, and flushed once more on `close`.

## Toolchain and checks

```sh
(cd sdks/java && mise trust && mise install)   # Temurin, Maven, Checkstyle and google-java-format, as mise.toml pins them

sdks/java/bin/lint            # Checkstyle and google-java-format, no warnings allowed
sdks/java/bin/test            # JUnit tests, every evaluation vector included
bun run conformance -- --service sdks/java/bin/test-service    # from the repository root, after bun install
```

`bin/test-service` builds the test service on first use. See [`test-service/README.md`](test-service/README.md) for
the accessor mapping, [`DESIGN.md`](DESIGN.md) for the design and [`RUN.md`](RUN.md) for the run that built this.
