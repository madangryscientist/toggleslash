# ToggleSlash Java server SDK

A server SDK for ToggleSlash in Java. It evaluates flags locally on a ruleset it keeps fresh from the
server, with no runtime dependency beyond the JDK. It follows the language-neutral
[SDK contract](../../docs/sdk-contract.md) and the [evaluation spec](../../docs/evaluation.md); the
design and the full mapping from the contract's names to Java's are in [`DESIGN.md`](DESIGN.md). It is
not published to any registry.

Java 17 or newer at run time. The repository builds it with the Temurin 25 toolchain pinned in
`mise.toml`.

## Quickstart

Build it from a checkout of this repository (`sdks/java`) and put `target/classes` on the class path:

```sh
(cd sdks/java && mise trust && mise install)
(cd sdks/java && mise exec -- mvn -B -q compile)
```

```java
import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Context;
import com.github.madangryscientist.toggleslash.sdks.java.Options;

try (Client client = Client.create(new Options().url("http://localhost:5000").sdkKey("fs_server_..."))) {
  client.ready();
  boolean on = client.boolVariation("new-checkout", Context.user("user-42"), false);
}
```

`create` never throws and does not wait for the network. `variation` and `variationDetail` never throw
and never do I/O: until the first ruleset has loaded they answer the fallback with reason
`NOT_READY`. `close` is idempotent and ends in about twice `requestTimeout` at most.

## Mapping

| Canonical | Java |
| --- | --- |
| `create` | `Client.create(Options)` |
| `ready` | `Client.ready()` (blocking), `Client.readyFuture()` |
| `variation` | `boolVariation`, `stringVariation`, `doubleVariation`, `longVariation`, `jsonVariation` |
| `variationDetail` | the same names with `Detail`, returning `Detail<T>` |
| `status` | `Client.status()` |
| `close` | `Client.close()` (`AutoCloseable`) |
| options | `Options.url`, `sdkKey`, `stream`, `pollInterval`, `flushInterval`, `requestTimeout`, `afterEvaluation`, `onError` |

`DESIGN.md` section 1 has the whole table, including defaults and the closed sets.

## Guarantees

- The SDK never throws at a caller and never blocks an evaluation on the network.
- A ruleset and its ETag swap in one step; a hook sees the ETag and flag version of the ruleset it
  evaluated against.
- The last good ruleset is kept through any failure.
- A context never leaves the process: no request, log, status or error carries one.
- Usage is counted per flag, variation and UTC day, sent in batches, and kept through a failed send.
- `Retry-After` on a 429 is honoured, capped at 300 seconds.
- Threads are daemon threads, so an unclosed client does not keep the JVM alive.

## Checks

From the repository root, with the toolchain installed as above and `bun install` run once:

```sh
sdks/java/bin/lint
sdks/java/bin/test
bun run conformance -- --service sdks/java/bin/test-service
```

`bin/test` builds, runs every evaluation vector and the native tests, and prints
`vectors: <n> pins: <n>`. `test-service/README.md` maps the conformance accessors.
