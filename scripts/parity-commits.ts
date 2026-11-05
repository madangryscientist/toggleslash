/**
 * Fails when a `feat` commit does not change docs/feature-parity.json.
 *
 * CI sets PARITY_BASE to the pull request's base SHA, or the previous tip on a push. Locally it
 * defaults to origin/main; when that ref is missing the check is skipped rather than failing a
 * developer who has not fetched it.
 */

import { execFileSync } from "node:child_process";

function git(args: string[]): string {
  return execFileSync("git", args, { encoding: "utf8" }).trim();
}

function tryGit(args: string[]): string | undefined {
  try {
    return git(args);
  } catch {
    return undefined;
  }
}

function main(): void {
  const configured = process.env.PARITY_BASE?.trim();
  const base = configured === undefined || configured === "" ? "origin/main" : configured;
  const mergeBase = tryGit(["merge-base", base, "HEAD"]);
  if (mergeBase === undefined) {
    console.log(`parity: skipping the commit check because "${base}" is not available`);
    return;
  }

  const subjects = git(["log", "--format=%s", `${mergeBase}..HEAD`])
    .split("\n")
    .filter((subject) => subject !== "");
  const features = subjects.filter((subject) => /^feat(\(|:)/.test(subject));
  if (features.length === 0) {
    console.log("parity: no feat commits in this range");
    return;
  }

  const changed = new Set(
    git(["diff", "--name-only", mergeBase, "HEAD"])
      .split("\n")
      .filter((file) => file !== ""),
  );
  if (changed.has("docs/feature-parity.json")) {
    console.log(`parity: ${features.length} feat commit(s) and the matrix changed`);
    return;
  }

  console.error("parity: these feat commits do not change docs/feature-parity.json:");
  for (const subject of features) console.error(`  - ${subject}`);
  console.error("Update the matrix, run `bun run parity`, and commit both files.");
  process.exitCode = 1;
}

main();
