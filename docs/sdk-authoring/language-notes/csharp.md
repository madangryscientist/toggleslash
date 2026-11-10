# C#

What the first C# run learned. Each fact cites that run: prompt `e3b07b5`, FS-98, 2026-11-10.

- **Toolchain.** Pin the newest LTS SDK (10.0.403, .NET 10, in that run) in `mise.toml`. The repository
  root's `global.json` pins a different SDK (10.0.402), so `sdks/csharp` carries its own `global.json` with
  the same number as `mise.toml` and `rollForward: disable`; without it `dotnet` here picks the root's.
- **Support dates.** .NET 8 and 9 both left support on 2026-11-10, the run date, so net10.0 was the minimum.
  Check Microsoft's support-policy page for the run date.
- **`close` from a continuation deadlocks.** `onError` and hooks run on the SDK's own threads, and a
  synchronous `Close()` joins them. A `TaskCompletionSource` completed inside `onError` runs its awaiting
  code inline on that thread by default, so a `Close()` there waits for itself. Create each such source with
  `TaskCreationOptions.RunContinuationsAsynchronously`, in tests and the test service too, and say so in the
  README. A direct `Close()` inside a callback is fine if the SDK marks its own thread.
- **`HttpClient` adds `Host`, and `Content-Length` on a POST, and nothing else** with automatic decompression
  off: no `User-Agent`, `Accept-Encoding` or `Connection`. A loopback capture showed it.
- **Fake time.** `Microsoft.Extensions.TimeProvider.Testing`'s `FakeTimeProvider` does not say how many timers
  are armed. Subclass it to count them, and advance only once the poll or hold loop and the flush loop have
  armed again. A fixed real pause between steps flaked under load (one failure in four `bin/test` runs).
  xunit runs test classes in parallel, so the suite loads itself.
- **No race detector.** A stress test of every operation from many threads with `Close` racing stands in.
- **Test service and layout.** `Microsoft.NET.Sdk.Web` (Kestrel from the shared framework, no package) as one
  more project in the solution, built by `bin/test` last. `UseArtifactsOutput` keeps build output under an
  ignored `artifacts/`, so `bin/` holds only the three scripts.
