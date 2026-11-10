# ToggleSlash server SDK for C#

Feature flags evaluated locally, in your process, from a ruleset the ToggleSlash server keeps current. It
meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md), has no runtime dependency outside the .NET base
class library (net10.0) and is **not published**: there is no package to install from a feed.

## Quickstart

Install from a checkout of this repository: reference the project from your own.

```sh
cd sdks/csharp
mise install            # the pinned .NET SDK, once
dotnet add <your project> reference sdks/csharp/src/ToggleSlash.Sdk/ToggleSlash.Sdk.csproj
```

Then:

```csharp
using ToggleSlash.Sdk;

// Read the key from the environment, never from source.
await using var client = ToggleSlashClient.Create(new ClientOptions
{
    Url = "https://flags.example.com",
    SdkKey = Environment.GetEnvironmentVariable("TOGGLESLASH_SDK_KEY"),
});

await client.ReadyAsync(); // optional: waits for the first load, never longer than the request timeout

var user = Context.ForKey("user-42", attributes: new Dictionary<string, object?> { ["plan"] = "pro" });
if (client.BoolVariation("new-checkout", user, fallback: false))
{
    // ...
}
// leaving the scope closes the client, which sends the last usage counts
```

`ToggleSlashClient.Create` returns at once and never throws. The `...Variation` methods and their `...VariationDetail`
twins never wait for the network: before the first load they answer your fallback with the reason `ERROR` /
`NOT_READY`. A client is safe to use from many threads. Options are `Stream`, `PollInterval`, `FlushInterval`,
`RequestTimeout`, `Hooks.AfterEvaluation` and `OnError`.

`OnError` runs on the SDK's own threads, and `Close()` waits for them. Calling `Close()` inside `OnError` is
fine (it starts the shutdown and returns), but if `OnError` completes a `TaskCompletionSource` whose awaiting
code then calls `Close()`, create it with `TaskCreationOptions.RunContinuationsAsynchronously`: the awaiting
code would otherwise run inline on the SDK's thread and `Close()` would wait for the very thread it is on.

## The API

Every name in the contract and its C# spelling is in the table in [`DESIGN.md`](DESIGN.md#1-the-mapping-table),
along with the design choices behind it.

## Guarantees

The contract's section 5, as this SDK keeps it:

1. It never throws at a caller, whatever the server, the network, a context, a hook or `OnError` does.
2. `...Variation`, `...VariationDetail` and `Status` never do I/O and never wait.
3. `ReadyAsync` settles after the first attempt and gives the same answer every time.
4. While the server is unreachable it keeps evaluating from the last good ruleset.
5. Only something with the shape of a ruleset replaces a ruleset.

It also sends no context, context key or attribute anywhere: usage counts carry a flag, a variation index and
a day.

## Running the checks

The toolchain is pinned in [`mise.toml`](mise.toml) and [`global.json`](global.json) (`mise install` from this
directory). The `bin` scripts find it themselves.

```sh
sdks/csharp/bin/lint      # dotnet format --verify-no-changes, and a build with the analyzers as errors
sdks/csharp/bin/test      # builds the solution, runs every evaluation vector and the native tests, builds the test service
bun run conformance -- --service sdks/csharp/bin/test-service   # from the repository root: the conformance harness
```

The test service is described in [`test-service/README.md`](test-service/README.md). A report of the run that
built this SDK is in [`RUN.md`](RUN.md).
