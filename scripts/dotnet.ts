import { existsSync } from "node:fs";
import { delimiter, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** The repository root, where `global.json` pins the .NET SDK. */
export const REPO_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..");

/** A `dotnet` that runs, and the SDK version it prints for `dotnet --version` here. */
export interface DotnetSdk {
  path: string;
  version: string;
}

/**
 * The first `dotnet` on `searchPath` (PATH by default) that runs here (from the repository root, so
 * `global.json` applies), with the version it printed, or null. Every one is tried, because one that
 * exists can still fail (a host with no SDK, a mise shim where none is active) and must not hide a
 * working one behind it. `scripts/moon.ts` and the e2e project both ask this, so they cannot
 * disagree about whether an SDK exists.
 */
export function findDotnetSdk(searchPath = process.env.PATH ?? ""): DotnetSdk | null {
  for (const dir of searchPath.split(delimiter)) {
    const candidate = join(dir, "dotnet");
    if (dir === "" || !existsSync(candidate)) continue;
    try {
      const ran = Bun.spawnSync([candidate, "--version"], {
        cwd: REPO_ROOT,
        stdout: "pipe",
        stderr: "ignore",
      });
      const version = ran.stdout.toString().trim();
      if (ran.success && version !== "") return { path: candidate, version };
    } catch {
      // Not runnable; try the next one.
    }
  }
  return null;
}

/** The path of the first `dotnet` on PATH that runs here, or null (see `findDotnetSdk`). */
export function findDotnet(): string | null {
  return findDotnetSdk()?.path ?? null;
}
