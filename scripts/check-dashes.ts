/**
 * The no-dash rule: no em dash (U+2014) or en dash (U+2013) in anything we write. It reads
 * every tracked file from the repo root (`bun scripts/check-dashes.ts`), so docs and data are
 * covered as well as code. The root `lint` script runs it after the Nx lint target; any hit
 * fails the gate. It stays outside Nx on purpose: the check reads the whole tree, and a cached
 * project-scoped target would replay a pass after a dash was added in `docs/`.
 */
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

const EM_OR_EN_DASH = /[\u2013\u2014]/;
const SKIPPED_PATHS = new Set(["apps/web/worker-configuration.d.ts", "bun.lock"]);

const tracked = execFileSync("git", ["ls-files", "-z"], { encoding: "utf8" })
  .split("\0")
  .filter((path) => path !== "");

const hits: string[] = [];
for (const path of tracked) {
  if (SKIPPED_PATHS.has(path) || path.startsWith("fixtures/")) continue;
  const text = readFileSync(path, "utf8");
  if (text.includes("\0")) continue;
  text.split("\n").forEach((line, i) => {
    if (EM_OR_EN_DASH.test(line)) {
      hits.push(`${path}:${i + 1}: em or en dash; use a full stop, comma, colon, brackets or "to"`);
    }
  });
}

if (hits.length > 0) {
  console.error(hits.join("\n"));
  process.exit(1);
}
