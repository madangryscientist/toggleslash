import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** The repository root, where `global.json` pins the .NET SDK. */
export const REPO_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");

/**
 * Whether a working `dotnet` runs here. It is run rather than looked up: a mise shim is on PATH
 * where no SDK is active (the same check as `scripts/nx.ts`).
 */
export function hasDotnet(): boolean {
  try {
    return Bun.spawnSync(["dotnet", "--version"], {
      cwd: REPO_ROOT,
      stdout: "ignore",
      stderr: "ignore",
    }).success;
  } catch {
    return false;
  }
}

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
