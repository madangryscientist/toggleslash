import { existsSync } from "node:fs";
import { delimiter, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** The repository root, where `global.json` pins the .NET SDK. */
export const REPO_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..");

/**
 * The first `dotnet` on PATH that runs here (from the repository root, so `global.json` applies), or
 * null. Every one is tried, because one that exists can still fail (a host with no SDK, a mise shim
 * where none is active) and must not hide a working one behind it. `scripts/nx.ts` and the e2e
 * project both ask this, so they cannot disagree about whether an SDK exists.
 */
export function findDotnet(): string | null {
  for (const dir of (process.env.PATH ?? "").split(delimiter)) {
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
