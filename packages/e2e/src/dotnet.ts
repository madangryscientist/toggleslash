import { findDotnet, REPO_ROOT } from "../../../scripts/dotnet.ts";

export { REPO_ROOT };

/** The first `dotnet` on PATH that runs, or null: the same search `scripts/moon.ts` makes. */
export const dotnetPath = (): string | null => findDotnet();

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
