import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** The repository root, where `global.json` pins the .NET SDK. */
export const REPO_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");

/**
 * The first `dotnet` on PATH that runs, or null. It is run rather than looked up: a mise shim is on
 * PATH where no SDK is active (the same check as `scripts/nx.ts`). Every `dotnet` on PATH is tried
 * in order because `nx run` puts `/usr/bin` ahead of the caller's PATH, and a host-only
 * `/usr/bin/dotnet` there hides the SDK a contributor put first.
 */
export function dotnetPath(): string | null {
  for (const dir of (process.env.PATH ?? "").split(":")) {
    const candidate = join(dir, "dotnet");
    if (dir === "" || !existsSync(candidate)) continue;
    try {
      const ran = Bun.spawnSync([candidate, "--version"], {
        cwd: REPO_ROOT,
        stdout: "ignore",
        stderr: "ignore",
      });
      if (ran.success) return candidate;
    } catch {
      // Not runnable; try the next one.
    }
  }
  return null;
}

/** Whether a working `dotnet` runs here. */
export const hasDotnet = (): boolean => dotnetPath() !== null;

/**
 * What to do when there is no SDK: skip with a printed line, except in CI, where `global.json` pins
 * the SDK and a missing one must fail the job rather than quietly drop the proof.
 */
export function skipReason(): string | null {
  if (hasDotnet()) return null;
  if (process.env.CI !== undefined) {
    throw new Error("dotnet SDK not found in CI (global.json pins it): not skipping the e2e");
  }
  const line = "dotnet SDK not found (none matching global.json): skipping the end-to-end test";
  process.stdout.write(`${line}\n`);
  return line;
}
