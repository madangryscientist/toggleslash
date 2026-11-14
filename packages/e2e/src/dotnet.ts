import { findDotnet, REPO_ROOT } from "../../../scripts/dotnet.ts";

export { REPO_ROOT };

/** The first `dotnet` on PATH that runs, or null: the same search `scripts/moon.ts` makes. */
export const dotnetPath = (): string | null => findDotnet();

/** Whether a working `dotnet` runs here. */
export const hasDotnet = (): boolean => dotnetPath() !== null;

/**
 * What to do when there is no SDK: skip with a printed line, except in CI, where `global.json` pins
 * the SDK and a missing one must fail the job rather than quietly drop the proof. A skip is cached
 * under `TOGGLESLASH_DOTNET=none`, which `scripts/moon.ts` sets; without it a skip would hash like a
 * run that had an SDK, so it fails instead (a failed run is never cached).
 */
export function skipReason(): string | null {
  if (hasDotnet()) return null;
  if (process.env.CI !== undefined) {
    throw new Error("dotnet SDK not found in CI (global.json pins it): not skipping the e2e");
  }
  if (process.env.TOGGLESLASH_DOTNET !== "none") {
    throw new Error(
      "dotnet SDK not found, and TOGGLESLASH_DOTNET is not `none`: run this through `bun scripts/moon.ts` or a root script, so the skip is cached apart from a real run",
    );
  }
  const line = "dotnet SDK not found (none matching global.json): skipping the end-to-end test";
  process.stdout.write(`${line}\n`);
  return line;
}
