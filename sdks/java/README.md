# ToggleSlash server SDK for Java

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It
meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md), has no runtime dependency outside the JDK (Java 17
or newer) and is **not published**: there is no artifact to fetch from a registry.

## Quickstart

Use it from a checkout of this repository. Install the pinned toolchain once, then build the SDK and put its
classes on your own class path:

```sh
(cd sdks/java && mise trust && mise install)   # Temurin, Maven, Checkstyle and google-java-format
(cd sdks/java && mise exec -- mvn -q compile)  # classes go to sdks/java/target/classes
```

Then:

```java
import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import java.util.Map;

// Read the key from the environment, never from source.
try (Client client =
    Client.create(
        Options.builder("https://flags.example.com", System.getenv("TOGGLESLASH_SDK_KEY")).build())) {
  client.ready(); // optional: waits for the first load, never longer than about twice the request timeout

  Map<String, Object> user = Map.of("key", "user-42", "plan", "pro");
  if (client.boolVariation("new-checkout", user, false)) {
    // ...
  }
} // leaving the block closes the client, which sends the last usage counts
```

`Client.create` returns at once and never throws. The `...Variation` methods and their `...VariationDetail`
twins never wait for the network: before the first load they answer your fallback with the reason `ERROR` /
`NOT_READY`. A client is safe to use from many threads. Options are `stream`, `pollInterval`,
`flushInterval`, `requestTimeout`, `afterEvaluation` and `onError`.

`onError` and the hook run on the SDK's own threads or the caller's, never with an SDK lock held, and may call
back into the client, including `close`.

## The API

Every name in the contract and its Java spelling is in the table in
[`DESIGN.md`](DESIGN.md#1-the-mapping-table), along with the design choices behind it.

## Guarantees

The contract's section 5, as this SDK keeps it:

1. It never throws at a caller, whatever the server, the network, a context, a hook or `onError` does.
2. `variation`, `variationDetail` and `status` never do I/O and never wait.
3. `ready` settles after the first attempt and gives the same answer every time.
4. While the server is unreachable it keeps evaluating from the last good ruleset.
5. Only something with the shape of a ruleset replaces a ruleset.

It also sends no context, context key or attribute anywhere: usage counts carry a flag, a variation index and
a day.

## Running the checks

The toolchain is pinned in [`mise.toml`](mise.toml). The `bin` scripts find it themselves.

```sh
sdks/java/bin/lint      # google-java-format (dry run), Checkstyle, and a compile with -Xlint:all -Werror
sdks/java/bin/test      # builds, runs every evaluation vector and the native tests, builds the test service
bun run conformance -- --service sdks/java/bin/test-service   # from the repository root: the conformance harness
```

The test service is described in [`test-service/README.md`](test-service/README.md). A report of the run that
built this SDK is in [`RUN.md`](RUN.md).
