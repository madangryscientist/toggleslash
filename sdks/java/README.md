# ToggleSlash server SDK for Java

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It meets
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), has **no runtime dependency** outside the JDK (Java 17 or
newer; the build runs on Temurin 25) and is **not published**: there is no artifact to download.

## Quickstart

Install from a checkout of this repository: build it once, then put `target/classes` (or the jar `mvn package`
makes) on your classpath.

```sh
cd sdks/java
mise trust && mise install      # the pinned JDK, Maven and lint tools, once
mise exec -- mvn -q -DskipTests package
```

Then:

```java
import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Context;
import com.github.madangryscientist.toggleslash.sdks.java.Options;

// Read the key from the environment, never from source.
try (Client client =
    Client.create(
        Options.builder()
            .url("https://flags.example.com")
            .sdkKey(System.getenv("TOGGLESLASH_SDK_KEY"))
            .build())) {
  client.ready(); // optional: waits for the first load, never longer than the request timeout

  Object user = Context.with(Context.of("user-42"), "plan", "pro");
  if (client.boolVariation("new-checkout", user, false)) {
    // ...
  }
} // closing the client sends the last usage counts
```

`Client.create` returns at once and never throws. The `...Variation` methods and their `...VariationDetail`
twins never wait for the network: before the first load they answer your fallback with the reason `ERROR` /
`NOT_READY`. A client is safe to use from many threads. A context is a plain `Map` (`Context` builds them); the
options are `url`, `sdkKey`, `stream`, `pollInterval`, `flushInterval`, `requestTimeout`, `afterEvaluation` and
`onError`.

`onError` and a hook run on the SDK's own threads or the caller's (see `DESIGN.md`). Calling `close()` inside
`onError` is fine: it starts the shutdown and returns without waiting for itself.

## The API

Every name in the contract and its Java spelling is in the table in [`DESIGN.md`](DESIGN.md#mapping-table-contract-section-2),
along with the design choices behind it.

## Guarantees

The contract's section 5, as this SDK keeps it:

1. It never throws at a caller, whatever the server, the network, a context, a hook or `onError` does.
2. `...Variation`, `...VariationDetail` and `status` never do I/O and never wait.
3. `ready` settles after the first attempt and gives the same answer every time.
4. While the server is unreachable it keeps evaluating from the last good ruleset.
5. Only something with the shape of a ruleset replaces a ruleset.

It also sends no context, context key or attribute anywhere: usage counts carry a flag, a variation index and a
day.

## Running the checks

The toolchain is pinned in [`mise.toml`](mise.toml) (`mise trust && mise install` from this directory). The
`bin` scripts run every tool through `mise exec` themselves.

```sh
sdks/java/bin/lint      # google-java-format (set-exit-if-changed) over every source root, and checkstyle
sdks/java/bin/test      # builds, runs every evaluation vector and the native tests, compiles the test service
bun run conformance -- --service sdks/java/bin/test-service   # from the repository root: the conformance harness
```

The test service is described in [`test-service/README.md`](test-service/README.md). A report of the run that
built this SDK is in [`RUN.md`](RUN.md).
