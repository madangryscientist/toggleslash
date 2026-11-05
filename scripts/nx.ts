#!/usr/bin/env bun
/**
 * `bun scripts/nx.ts <nx args>`: nx, with `apps/server` skipped when there is no .NET SDK.
 * The `@nx/dotnet` plugin fails every nx command without an SDK, so a contributor who only
 * touches the TS projects could not run the gate; `NX_DOTNET_DISABLE=true` takes the dotnet
 * projects out of the graph. With a working `dotnet` nothing changes. The skip is printed, not silent.
 */
function hasDotnet(): boolean {
  try {
    // Run it rather than look it up: a mise shim is on PATH where no SDK is active.
    return Bun.spawnSync(["dotnet", "--version"], { stdout: "ignore", stderr: "ignore" }).success;
  } catch {
    return false;
  }
}

const env: Record<string, string | undefined> = { ...process.env };
if (env.NX_DOTNET_DISABLE === undefined && !hasDotnet()) {
  // CI installs the SDK; a missing one there must fail the gate, not quietly drop the server from it.
  if (env.CI !== undefined) {
    process.stderr.write(
      "dotnet SDK not found in CI (global.json pins it): not skipping apps/server\n",
    );
    process.exit(1);
  }
  env.NX_DOTNET_DISABLE = "true";
  console.log("dotnet SDK not found (none matching global.json): skipping apps/server");
}

const nx = Bun.spawnSync(["nx", ...process.argv.slice(2)], {
  env,
  stdin: "inherit",
  stdout: "inherit",
  stderr: "inherit",
});
process.exit(nx.exitCode ?? 1);
