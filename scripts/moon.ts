#!/usr/bin/env bun
/**
 * `bun scripts/moon.ts <moon args>`: moon, with the .NET SDK found for it and the `dotnet`-tagged
 * project (`apps/server`) left out, loudly, when there is none.
 *
 * A contributor who only touches the TS projects must still be able to run the gate, so with no
 * working `dotnet` the run is limited to the projects not tagged `dotnet` (`--query "tag!=dotnet"`,
 * added to `moon run`); with one, the gate runs the server's tests too. `TOGGLESLASH_DOTNET_DISABLE`
 * set to any value skips the search and the banner and leaves the server out, quietly.
 *
 * Which `dotnet` a task runs is decided here, not left to PATH. moon puts only its own bin dirs and
 * `node_modules/.bin` directories ahead of the caller's PATH, so the first `dotnet` on the caller's
 * PATH wins, and on this machine that is `/usr/bin/dotnet`, a host with no SDK, ahead of the mise
 * SDK. The directory of the first `dotnet` that works goes first on PATH.
 */
import { delimiter, dirname, join } from "node:path";
import { findDotnet, REPO_ROOT } from "./dotnet.ts";

const args = process.argv.slice(2);
const env: Record<string, string | undefined> = { ...process.env };
// Setting TOGGLESLASH_DOTNET_DISABLE yourself skips the search and the banner.
const disabled = env.TOGGLESLASH_DOTNET_DISABLE !== undefined;
const dotnet = disabled ? null : findDotnet();
let banner: string | null = null;

if (dotnet !== null) {
  env.PATH = `${dirname(dotnet)}${delimiter}${env.PATH ?? ""}`;
} else if (!disabled) {
  // CI installs the SDK; a missing one there must fail the gate, not quietly drop the server from it.
  if (env.CI !== undefined) {
    process.stderr.write(
      "dotnet SDK not found in CI (global.json pins it): not skipping apps/server\n",
    );
    process.exit(1);
  }
  banner = [
    "",
    "!! dotnet SDK not found (none matching global.json): apps/server is NOT in this run.",
    "!! The C# server and evaluator tests and the end-to-end test did not run, so green here does not cover them.",
    "!! `mise install` installs the pinned SDK; put it on PATH (`mise activate`, or `mise exec -- bun run test`).",
    "",
    "",
  ].join("\n");
}

if (dotnet === null && args[0] === "run") {
  // Before any `--`, which hands the rest to the task itself.
  const passthrough = args.indexOf("--");
  args.splice(passthrough === -1 ? args.length : passthrough, 0, "--query", "tag!=dotnet");
}

// Printed before moon starts and again after it ends, so the last line on screen is not moon's success.
if (banner !== null) process.stderr.write(banner);
const moon = Bun.spawnSync([process.execPath, join(REPO_ROOT, "node_modules/.bin/moon"), ...args], {
  env,
  stdin: "inherit",
  stdout: "inherit",
  stderr: "inherit",
});
if (banner !== null) process.stderr.write(banner);
process.exit(moon.exitCode ?? 1);
