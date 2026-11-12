# ToggleSlash server SDK for Java

The Java server SDK of [ToggleSlash](../../README.md). It follows the language-neutral
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates flags locally, to
[`docs/evaluation.md`](../../docs/evaluation.md), against a ruleset it keeps current from the server. It has no
runtime dependency (the JDK's own HTTP client and nothing else), needs Java 17 or newer, and is not published
anywhere: use it from a checkout of this repository.

## Quickstart

```java
import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import java.util.Map;

try (Client client =
    Client.create(
        Options.builder(
                "https://toggleslash.example.invalid", // the server's base URL
                "sdk-key-from-the-dashboard") // a server SDK key
            .build())) {
  if (client.ready()) { // waits for the first load attempt, bounded by requestTimeout
    System.out.println("ruleset loaded");
  }
  Map<String, Object> context = Map.of("key", "user-42", "plan", "pro");
  if (client.booleanVariation("new-checkout", context, false)) {
    System.out.println("new checkout");
  }
  Detail<String> detail = client.stringVariationDetail("banner-text", context, "welcome");
  System.out.println(detail.value() + " " + detail.variationIndex() + " " + detail.reason());
} // close flushes usage counts and stops the background threads
```

## Mapping from the contract

The full table (every name, option and default, and what is a `Detail`, a `Status` and a `StreamState`) is
section 1 of [`DESIGN.md`](DESIGN.md). In short: `Client.create(Options)`, `client.ready()`,
`client.booleanVariation(...)`, `stringVariation`, `longVariation` (the integer accessor), `doubleVariation`,
`jsonVariation` and the generic `client.variation(flagKey, context, fallback)`, each with a `...Detail` twin,
`client.status()` and `client.close()`; options `stream`, `pollInterval`, `flushInterval`, `requestTimeout`,
`afterEvaluation` and `onError` on `Options.builder(url, sdkKey)`.

## Guarantees

- Nothing is thrown at a caller: a failure becomes your fallback with an `ERROR` reason, goes to `onError`, or both.
- `variation`, `variationDetail` and `status` read memory only: they never wait on the network, a load or a flush.
- `ready()` settles after the first attempt, is bounded by `requestTimeout`, and gives the same answer each time.
- The SDK keeps evaluating from the last good ruleset while the server is unreachable; only a valid ruleset replaces one.
- No evaluation context, attribute or SDK key appears in a request body or URL (the key is the `Authorization`
  header only), a log line, a status or an error the SDK builds. The SDK writes no log line at all.
- `close()` is safe to call twice and from many threads, and returns within about twice `requestTimeout`.
- The SDK's threads are daemon threads: a client nobody closed does not keep the process alive.

## Check it

From the root of the repository:

```sh
sdks/java/bin/lint                                            # google-java-format --set-exit-if-changed and Checkstyle
sdks/java/bin/test                                            # JUnit, and every evaluation vector
bun run conformance -- --service sdks/java/bin/test-service   # the shared conformance harness
```

`mise.toml` pins Temurin, Maven, Checkstyle and google-java-format; with `mise` installed the scripts use those
versions. See [`test-service/README.md`](test-service/README.md) for the conformance service,
[`DESIGN.md`](DESIGN.md) for the design decisions and [`RUN.md`](RUN.md) for the report of the run that wrote
this SDK.
