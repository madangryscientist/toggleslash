#!/usr/bin/env bun
/**
 * `bun scripts/nx.ts <nx args>`: nx, with the .NET SDK found for it and `apps/server` skipped,
 * loudly, when there is none.
 *
 * The `@nx/dotnet` plugin fails every nx command without an SDK, so a contributor who only touches
 * the TS projects could not run the gate; `NX_DOTNET_DISABLE=true` takes the dotnet projects out of
 * the graph. With a working `dotnet` the gate runs the server's tests.
 *
 * Which `dotnet` nx runs is decided here, not left to PATH. Nx runs every task with the directory of
 * its own runtime put ahead of PATH (npm-run-path), and `nx` run through its `#!/usr/bin/env node`
 * shebang is `/usr/bin/node` on this machine, so `/usr/bin` went first and `/usr/bin/dotnet` (a host
 * with no SDK) hid the mise SDK even with it first on the caller's PATH. So nx runs under Bun
 * (`~/.bun/bin`, no `dotnet` in it) and the directory of the first `dotnet` that works goes first.
 */
import { delimiter, dirname, join } from "node:path";
import { findDotnet, REPO_ROOT } from "./dotnet.ts";

const env: Record<string, string | undefined> = { ...process.env };
// Setting NX_DOTNET_DISABLE yourself skips the search and the banner.
const dotnet = env.NX_DOTNET_DISABLE === undefined ? findDotnet() : null;
let banner: string | null = null;

if (dotnet !== null) {
  env.PATH = `${dirname(dotnet)}${delimiter}${env.PATH ?? ""}`;
} else if (env.NX_DOTNET_DISABLE === undefined) {
  // CI installs the SDK; a missing one there must fail the gate, not quietly drop the server from it.
  if (env.CI !== undefined) {
    process.stderr.write(
      "dotnet SDK not found in CI (global.json pins it): not skipping apps/server\n",
    );
    process.exit(1);
  }
  env.NX_DOTNET_DISABLE = "true";
  banner = [
    "",
    "!! dotnet SDK not found (none matching global.json): apps/server is NOT in this run.",
    "!! The C# server and evaluator tests and the end-to-end test did not run, so green here does not cover them.",
    "!! `mise install` installs the pinned SDK; put it on PATH (`mise activate`, or `mise exec -- bun run test`).",
    "",
    "",
  ].join("\n");
}

// Printed before nx starts and again after it ends, so the last line on screen is not nx's success.
if (banner !== null) process.stderr.write(banner);
const nx = Bun.spawnSync(
  [process.execPath, join(REPO_ROOT, "node_modules/.bin/nx"), ...process.argv.slice(2)],
  {
    env,
    stdin: "inherit",
    stdout: "inherit",
    stderr: "inherit",
  },
);
if (banner !== null) process.stderr.write(banner);
process.exit(nx.exitCode ?? 1);
