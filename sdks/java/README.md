# ToggleSlash server SDK for Java

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It
meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md), has no runtime dependency outside the JDK
(`java.net.http` for the network) and is **not published**: there is no artifact to fetch from a repository.
Java 17 or newer.

## Quickstart

Install from a checkout of this repository, into your local Maven repository:

```sh
cd sdks/java
mise trust && mise install      # the pinned JDK, Maven and Checkstyle, once
mise exec -- mvn -B -q -DskipTests install
```

Then depend on `com.github.madangryscientist.toggleslash:toggleslash-sdk-java:0.0.0`. Use it:

```java
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.ToggleSlashClient;
import java.util.Map;

// Read the key from the environment, never from source.
try (ToggleSlashClient client =
    ToggleSlashClient.create(
        Options.builder("https://flags.example.com", System.getenv("TOGGLESLASH_SDK_KEY")).build())) {
  client.ready(); // optional: waits for the first load, never longer than twice the request timeout

  Map<String, Object> user = Map.of("key", "user-42", "plan", "pro");
  if (client.boolVariation("new-checkout", user, false)) {
    // ...
  }
} // leaving the block closes the client, which sends the last usage counts
```

The context is a map of JSON values (`null`, `Boolean`, `Number`, `String`, `List`, `Map`): a single
context has a `key`, a multi-context has `kind` set to `multi` (contract section 3). Anything else answers
the fallback with `ERROR` / `INVALID_CONTEXT`.

`ToggleSlashClient.create` returns at once and never throws. The `...Variation` methods and their
`...VariationDetail` twins never wait for the network: before the first load they answer your fallback with the
reason `ERROR` / `NOT_READY`. A client is safe to use from many threads. Options are `stream`, `pollInterval`,
`flushInterval`, `requestTimeout`, `afterEvaluation` and `onError`. The two SDK threads are daemon threads, so
they do not keep the process alive; `close()` still sends the last usage counts.

`afterEvaluation` and `onError` run on the thread that saw the event (the SDK's, or yours for a hook), with no
SDK lock held, so a callback may call `status`, `variation` or `close`. `ready` called from a callback
answers from what is held and does not wait.

## The API

Every name in the contract and its Java spelling is in the table in
[`DESIGN.md`](DESIGN.md#mapping-table), along with the design choices behind it. Typed accessors:
`boolVariation`, `stringVariation`, `doubleVariation`, `intVariation` (a `long`: a fraction or a value outside
signed 64 bits answers `TYPE_MISMATCH`) and the generic `variation`, which takes its kind from the fallback.

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

The toolchain is pinned in [`mise.toml`](mise.toml) (`mise trust && mise install` from this directory). The
`bin` scripts run their tools through `mise exec` themselves. The first run needs the network once, to fetch
the Maven plugins.

```sh
sdks/java/bin/lint      # Spotless (google-java-format) over every Java file, Checkstyle over src/main/java
sdks/java/bin/test      # compiles everything, runs every evaluation vector and the native tests
bun run conformance -- --service sdks/java/bin/test-service   # from the repository root: the conformance harness
```

`bin/lint` prints four `WARNING: sun.misc.Unsafe` lines from Spotless's own formatter code on JDK 25; they
are not about this SDK's code and are left visible.

The test service is described in [`test-service/README.md`](test-service/README.md). A report of the run that
built this SDK is in [`RUN.md`](RUN.md).
