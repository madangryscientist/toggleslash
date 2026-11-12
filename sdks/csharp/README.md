# ToggleSlash server SDK for C#

The C# server SDK for ToggleSlash. It evaluates flags locally, to [`docs/evaluation.md`](../../docs/evaluation.md),
over a ruleset it keeps current from the server, and keeps the contract in
[`docs/sdk-contract.md`](../../docs/sdk-contract.md). Assembly and package id `ToggleSlash.Sdk`, namespace
`ToggleSlash.Sdk`, target `net10.0`, no dependency beyond the shared framework. Nothing is published and no
registry is named: use it from a checkout of this repository.

## Quickstart

From a checkout, reference the project (the install):

```sh
dotnet add <your project> reference /path/to/toggleslash/sdks/csharp/src/ToggleSlash.Sdk/ToggleSlash.Sdk.csproj
```

```csharp
using ToggleSlash.Sdk;

using var client = ToggleSlashClient.Create(new ClientOptions
{
    Url = "https://flags.example.test",
    SdkKey = Environment.GetEnvironmentVariable("TOGGLESLASH_SDK_KEY"),
});

client.Ready(); // optional: waits for the first load, bounded by the request timeout

var user = new Dictionary<string, object?> { ["key"] = "u1", ["plan"] = "pro" };
if (client.BoolVariation("new-checkout", user, false))
{
    Console.WriteLine("new checkout");
}

var detail = client.StringVariationDetail("banner", user, "none");
Console.WriteLine($"{detail.Value} {detail.VariationIndex} {detail.Reason.Kind}");
```

`Create` returns at once. Before the first load, and when a flag is missing, malformed or of another type, an
accessor answers the fallback, and the `Detail` form says why. The leaving `using` calls `Close`.

A context is a dictionary with string keys and a `key` entry, with strings, booleans, every .NET number type and
nested lists and dictionaries as attribute values. A value the SDK cannot send as JSON
gives the fallback with `INVALID_CONTEXT`.

## The mapping

The canonical names of the contract map to C# in the table in [`DESIGN.md`](DESIGN.md#mapping-table-contract-section-2),
with the closed sets (reason kinds, error codes, stream states, `lastError` codes) and their spelling.
`DESIGN.md` also holds the concurrency model, the HTTP and stream handling, the dependencies (none), the JSON type
and the package layout.

## Guarantees

From contract section 5:

- No operation throws at the caller, whatever the server, the network or the context does.
- An evaluation never waits on the network: it reads the ruleset held, or answers `NOT_READY` before the first load.
- `Ready` settles after the first attempt, bounded, and may be called again.
- An answer that is not a ruleset never replaces the one held, and a ruleset and its ETag are swapped together.
- Every operation is safe from many threads at once, and `Close` is safe to call twice or concurrently.
- A callback that throws, or calls back into the client, neither breaks the client nor deadlocks it.
- No context, context key or attribute appears in a request, an error or a log line.
- Usage counts are batched, kept through a failure the server may recover from, and flushed once more on `Close`.

## Callbacks and `Close`

`OnError` and the hooks run on the SDK's own threads (or the caller's), and a synchronous `Close()` joins those
threads. A direct `Close()` inside `OnError` is safe: the SDK marks its own thread, starts the shutdown and returns.
A `TaskCompletionSource` completed inside a callback would run its awaiting code inline on that thread, and a
`Close()` there would wait for itself. **Create each such source with
`TaskCreationOptions.RunContinuationsAsynchronously`**, as the SDK, its tests and its test service do.

## Toolchain and checks

```sh
(cd sdks/csharp && mise trust && mise install)   # the .NET SDK, as sdks/csharp/mise.toml and global.json pin it

sdks/csharp/bin/lint            # dotnet format (whitespace, style, analyzers) and a build with warnings as errors
sdks/csharp/bin/test            # builds everything (the test service last), runs the unit tests and every evaluation vector
bun run conformance -- --service sdks/csharp/bin/test-service    # from the repository root, after bun install
```

Build output goes under `sdks/csharp/artifacts/` (ignored), so `bin/` holds only the three scripts. There is no race
detector for .NET; a stress test of every operation from many threads with `Close` racing stands in
(`StressTests.EveryOperationFromManyThreadsWithCloseRacingNeverThrowsOrHangs`). `sdks/csharp/test-service` is the
conformance test service; see its [`README.md`](test-service/README.md) for the accessor mapping.
