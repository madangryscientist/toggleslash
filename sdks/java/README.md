# ToggleSlash server SDK for Java

The ToggleSlash server SDK for Java 17 or newer. It fetches an environment's ruleset, keeps it fresh from the
stream (with a polling fallback), evaluates flags locally to [`docs/evaluation.md`](../../docs/evaluation.md),
and sends usage counts in batches. It meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md).

**Nothing is published.** There is no Maven Central artifact and no registry entry; build it from a checkout.
Design and the name mapping are in [`DESIGN.md`](DESIGN.md); what the run that built it decided and guessed is in
[`RUN.md`](RUN.md).

## Install from a checkout

```bash
(cd sdks/java && mise trust && mise install)   # Temurin 25 and Maven 3.10.0, as mise.toml pins them
cd sdks/java && mise exec -- mvn -B -q -ntp install -DskipTests
```

Then depend on `com.github.madangryscientist.toggleslash:toggleslash-sdk-java` from your local repository. The
SDK has no runtime dependency beyond the JDK (`java.net.http`); it compiles with `--release 17`.

## Use

```java
import com.github.madangryscientist.toggleslash.sdks.java.ClientOptions;
import com.github.madangryscientist.toggleslash.sdks.java.ToggleSlashClient;
import java.util.Map;

try (ToggleSlashClient client =
    ToggleSlashClient.create(
        ClientOptions.builder("https://toggleslash.example", System.getenv("TOGGLESLASH_SDK_KEY"))
            .onError(e -> System.err.println("toggleslash: " + e.getMessage()))
            .build())) {
  client.ready(); // optional: waits for the first ruleset attempt, then answers true or false
  Map<String, Object> context = Map.of("key", "user-42", "plan", "pro");
  boolean on = client.boolVariation("new-checkout", context, false);
  var detail = client.stringVariationDetail("banner", context, "none");
  System.out.println(on + " " + detail.value() + " " + detail.reason());
} // close() flushes the usage counts that are pending
```

`create` returns at once and never waits for the network. Until a ruleset is held every call answers its
fallback with reason `ERROR` and code `NOT_READY`. The accessors are `boolVariation`, `stringVariation`,
`intVariation` (a `long`), `doubleVariation` and `jsonVariation`, each with a `...Detail` twin returning
`Evaluation<T>` (`value()`, `variationIndex()`, `reason()`). `status()` reports the ruleset's ETag, the last
sync, the stream state and the last error.

## What it guarantees

- No call throws at the caller: a bad context, a missing flag, a type mismatch or a throwing hook gives the
  fallback and, where it matters, one `onError` call.
- `variation*` and `status` never wait on the network or on a lock held across I/O. One thread's hung request
  cannot slow another's evaluation.
- The ruleset and its ETag are swapped together, so an event's ETag always belongs to the version it reports.
- The client's threads are daemons: a process that never calls `close` still exits.
- `close` is idempotent, safe from many threads and from inside a callback, and bounded by `requestTimeout`.
  It sends one last usage flush, and reports through `onError` what it could not send.
- `Retry-After` is honoured (capped at 300 s), with one hold for the ruleset and stream and a separate one for
  usage.
- The context, its key and its attributes never leave the process: usage requests carry counts only.

## Check it

From `sdks/java`:

```bash
bin/lint            # Spotless (google-java-format) and Checkstyle, no warnings allowed
bin/test            # javac -Xlint:all -Werror, JUnit 6, and every evaluation vector; prints the vector counts
```

From the repository root, the cross-language harness drives the SDK through its test service
([`test-service/README.md`](test-service/README.md)):

```bash
bun run conformance -- --service sdks/java/bin/test-service
```

The first Maven run needs the network to fill `~/.m2`; later runs work offline (`mvn -o`).
