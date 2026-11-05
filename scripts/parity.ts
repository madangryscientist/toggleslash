/** Generate or check docs/feature-parity.md from docs/feature-parity.json. */

import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { renderParity } from "./parity-render.ts";
import { parseParity, type ParityData } from "./parity-schema.ts";
import { checkSurfaces, loadSurfaces } from "./parity-surfaces.ts";

const here = fileURLToPath(new URL(".", import.meta.url));
const root = join(here, "..");
const dataPath = join(root, "docs/feature-parity.json");
const outputPath = join(root, "docs/feature-parity.md");

function load(): ParityData {
  const raw: unknown = JSON.parse(readFileSync(dataPath, "utf8"));
  return parseParity(raw);
}

function validate(data: ParityData): void {
  const errors = checkSurfaces(data, loadSurfaces(root), root);
  if (errors.length === 0) return;
  for (const error of errors) console.error(`parity: ${error}`);
  throw new Error("the feature parity data does not match the repository surfaces");
}

function report(data: ParityData): void {
  const yes = data.features.filter((feature) => feature.featureslash.status === "yes").length;
  const partial = data.features.filter(
    (feature) => feature.featureslash.status === "partial",
  ).length;
  const no = data.features.filter((feature) => feature.featureslash.status === "no").length;
  console.log(
    `parity: ${data.features.length} capabilities (${yes} yes, ${partial} partial, ${no} no)`,
  );
}

function write(data: ParityData): void {
  writeFileSync(outputPath, renderParity(data));
  report(data);
  console.log(`parity: wrote ${outputPath}`);
}

function check(data: ParityData): void {
  const expected = renderParity(data);
  let actual: string;
  try {
    actual = readFileSync(outputPath, "utf8");
  } catch {
    throw new Error("docs/feature-parity.md is missing; run `bun run parity`.");
  }
  if (actual !== expected) {
    throw new Error("docs/feature-parity.md is stale; run `bun run parity` and commit the result.");
  }
  report(data);
  console.log("parity: docs/feature-parity.md is up to date");
}

function main(): void {
  let data: ParityData;
  try {
    data = load();
    validate(data);
    if (process.argv.includes("--write")) write(data);
    else check(data);
  } catch (error) {
    console.error(`parity: ${error instanceof Error ? error.message : String(error)}`);
    process.exitCode = 1;
  }
}

main();
