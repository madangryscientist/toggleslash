/** Loads the surfaces a feature can appear on, and checks the parity data against them. */

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { COMMANDS } from "../packages/cli/src/commands/index.ts";
import type { ParityData } from "./parity-schema.ts";

export interface Surfaces {
  api: Set<string>;
  cli: Set<string>;
  dashboard: Set<string>;
}

interface UsedSurfaces {
  api: Set<string>;
  cli: Set<string>;
  dashboard: Set<string>;
}

function loadOpenApi(root: string): Set<string> {
  const path = join(root, "apps/server/openapi/v1.json");
  const raw: unknown = JSON.parse(readFileSync(path, "utf8"));
  if (typeof raw !== "object" || raw === null || !("paths" in raw)) {
    throw new Error(`${path}: no paths object.`);
  }
  const paths = raw.paths;
  if (typeof paths !== "object" || paths === null || Array.isArray(paths)) {
    throw new Error(`${path}: paths must be an object.`);
  }
  return new Set(Object.keys(paths));
}

function loadDashboard(root: string): Set<string> {
  const dir = join(root, "apps/web/src/routes");
  const routes = new Set<string>();
  for (const name of readdirSync(dir)) {
    if (!name.endsWith(".tsx") || name === "__root.tsx" || name.startsWith("-")) continue;
    const base = name.slice(0, -".tsx".length);
    routes.add(base === "index" ? "/" : `/${base.replaceAll(".", "/")}`);
  }
  return routes;
}

export function loadSurfaces(root: string): Surfaces {
  return {
    api: loadOpenApi(root),
    cli: new Set(COMMANDS.map((command) => command.name)),
    dashboard: loadDashboard(root),
  };
}

function splitEvidence(entry: string): { kind: string; target: string } {
  const at = entry.indexOf(":");
  if (at < 1 || at === entry.length - 1) {
    throw new Error(`evidence "${entry}" must be "<kind>:<target>".`);
  }
  return { kind: entry.slice(0, at), target: entry.slice(at + 1) };
}

interface EvidenceRef {
  entry: string;
  kind: string;
  target: string;
}

function checkNamed(ref: EvidenceRef, surfaces: Surfaces, used: UsedSurfaces): string | undefined {
  if (ref.kind === "api") {
    used.api.add(ref.target);
    return surfaces.api.has(ref.target)
      ? undefined
      : `evidence "${ref.entry}" names an API route the OpenAPI document does not have.`;
  }
  if (ref.kind === "cli") {
    used.cli.add(ref.target);
    return surfaces.cli.has(ref.target)
      ? undefined
      : `evidence "${ref.entry}" names a CLI command that does not exist.`;
  }
  used.dashboard.add(ref.target);
  return surfaces.dashboard.has(ref.target)
    ? undefined
    : `evidence "${ref.entry}" names a dashboard route that does not exist.`;
}

function checkOne(
  entry: string,
  surfaces: Surfaces,
  used: UsedSurfaces,
  root: string,
): string | undefined {
  const { kind, target } = splitEvidence(entry);
  if (kind === "api" || kind === "cli" || kind === "dashboard") {
    return checkNamed({ entry, kind, target }, surfaces, used);
  }
  if (kind === "path" || kind === "doc" || kind === "test") {
    return existsSync(join(root, target))
      ? undefined
      : `evidence "${entry}" points at a path that does not exist.`;
  }
  return `evidence "${entry}" has an unknown kind "${kind}"; use api, cli, dashboard, path, doc or test.`;
}

function checkCoverage(surfaces: Surfaces, used: UsedSurfaces): string[] {
  const errors: string[] = [];
  for (const path of surfaces.api) {
    if (path === "/health") continue;
    if (!used.api.has(path)) errors.push(`API route ${path} is not claimed by any feature.`);
  }
  for (const command of surfaces.cli) {
    if (!used.cli.has(command))
      errors.push(`CLI command "${command}" is not claimed by any feature.`);
  }
  for (const route of surfaces.dashboard) {
    if (!used.dashboard.has(route))
      errors.push(`Dashboard route ${route} is not claimed by any feature.`);
  }
  return errors;
}

export function checkSurfaces(data: ParityData, surfaces: Surfaces, root: string): string[] {
  const errors: string[] = [];
  const used: UsedSurfaces = { api: new Set(), cli: new Set(), dashboard: new Set() };
  for (const feature of data.features) {
    for (const entry of feature.featureslash.evidence) {
      const error = checkOne(entry, surfaces, used, root);
      if (error !== undefined) errors.push(`${feature.id}: ${error}`);
    }
  }
  errors.push(...checkCoverage(surfaces, used));
  return errors;
}
