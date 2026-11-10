# ToggleSlash server SDK for Java

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It
meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md), has no runtime dependency outside the JDK
(Java 17 or later) and is **not published**: there is no version to install from a registry.

## Quickstart

Install from a checkout of this repository: build it and put the jar on your class path.

```sh
cd sdks/java
mise install            # the pinned Java and Maven, once
mise exec -- mvn -B -q -DskipTests package   # writes target/toggleslash-sdk-java-0.1.0-SNAPSHOT.jar
```

Then:

```java
import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Context;
import com.github.madangryscientist.toggleslash.sdks.java.Options;

public final class Main {
  public static void main(String[] args) {
    // Read the key from the environment, never from source.
    Options options =
        Options.builder("https://flags.example.com", System.getenv("TOGGLESLASH_SDK_KEY")).build();
    try (Client client = Client.create(options)) { // close() sends the last usage counts
      client.ready(); // optional: waits for the first load, never longer than the request timeout
      Context user = Context.user("user-42").with("plan", "pro");
      if (client.boolVariation("new-checkout", user, false)) {
        // ...
      }
    }
  }
}
```

`Client.create` returns at once and never throws. `...Variation` and its `...VariationDetail` twins never wait
for the network: before the first load they answer your fallback with the reason `ERROR` / `NOT_READY`. A
client is safe to use from many threads. Options are `stream`, `pollInterval`, `flushInterval`,
`requestTimeout`, `afterEvaluation` and `onError`.

## The API

Every name in the contract and its Java spelling is in the table in
[`DESIGN.md`](DESIGN.md#the-mapping-table), along with the design choices behind it.

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

The toolchain is pinned in [`mise.toml`](mise.toml) (`mise install` from this directory). The `bin` scripts
find it themselves.

```sh
sdks/java/bin/lint      # google-java-format --dry-run, Checkstyle, javac -Xlint:all -Werror
sdks/java/bin/test      # mvn clean test: every evaluation vector, the native tests, and the test service's build
bun run conformance -- --service sdks/java/bin/test-service   # from the repository root: the conformance harness
```

The test service is described in [`test-service/README.md`](test-service/README.md). A report of the run that
built this SDK is in [`RUN.md`](RUN.md).
