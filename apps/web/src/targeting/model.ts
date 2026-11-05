/**
 * A flag's per-environment config as the admin API stores it (docs/evaluation.md), and a defensive
 * reader for the server's body. `draft.ts` is the same thing as a person edits it. Pure: no React, no fetch.
 */
import { isRecord, type Json } from "../api/json.ts";

export type ClauseValue = string | number | boolean;

export interface Clause {
  op: string;
  attribute: string;
  values: ClauseValue[];
  negate: boolean;
}

export interface RolloutEntry {
  variation: number;
  /** Out of {@link WEIGHT_TOTAL}. */
  weight: number;
}

export type Serve =
  | { kind: "variation"; variation: number }
  | { kind: "rollout"; entries: RolloutEntry[] };

export interface Rule {
  clauses: Clause[];
  serve: Serve;
}

export interface Target {
  variation: number;
  contextKeys: string[];
}

export interface Prerequisite {
  flagKey: string;
  variation: number;
}

export interface Config {
  on: boolean;
  offVariation: number | null;
  targets: Target[];
  prerequisites: Prerequisite[];
  rules: Rule[];
  fallthrough: Serve;
  version: number;
}

/** Rollout weights sum to this; one percent is 1000. */
export const WEIGHT_TOTAL = 100_000;

export const ATTRIBUTE_OPS = [
  "in",
  "contains",
  "startsWith",
  "endsWith",
  "lt",
  "lte",
  "gt",
  "gte",
] as const;
export const SEGMENT_OP = "segmentMatch";

function array(value: unknown): Json[] {
  return Array.isArray(value) ? value.filter(isRecord) : [];
}

function whole(value: unknown, fallback = 0): number {
  return typeof value === "number" && Number.isFinite(value) ? Math.trunc(value) : fallback;
}

function clauseValues(value: unknown): ClauseValue[] {
  if (!Array.isArray(value)) return [];
  const out: ClauseValue[] = [];
  for (const v of value) {
    if (typeof v === "string" || typeof v === "boolean") out.push(v);
    else if (typeof v === "number" && Number.isFinite(v)) out.push(v);
  }
  return out;
}

function parseServe(raw: unknown): Serve {
  const holder = isRecord(raw) ? raw : {};
  const rollout = holder["rollout"];
  if (isRecord(rollout)) {
    return {
      kind: "rollout",
      entries: array(rollout["variations"]).map((e) => ({
        variation: whole(e["variation"]),
        weight: whole(e["weight"]),
      })),
    };
  }
  return { kind: "variation", variation: whole(holder["variation"]) };
}

/** The server's config body, read defensively: anything missing is empty, so a page never throws on it. */
export function parseConfig(raw: unknown): Config | null {
  if (!isRecord(raw) || typeof raw["on"] !== "boolean") return null;
  return {
    on: raw["on"],
    offVariation: typeof raw["offVariation"] === "number" ? whole(raw["offVariation"]) : null,
    targets: array(raw["targets"]).map((t) => ({
      variation: whole(t["variation"]),
      contextKeys: Array.isArray(t["contextKeys"])
        ? t["contextKeys"].filter((k) => typeof k === "string")
        : [],
    })),
    prerequisites: array(raw["prerequisites"]).map((p) => ({
      flagKey: typeof p["flagKey"] === "string" ? p["flagKey"] : "",
      variation: whole(p["variation"]),
    })),
    rules: array(raw["rules"]).map((r) => ({
      clauses: array(r["clauses"]).map((c) => ({
        op: typeof c["op"] === "string" ? c["op"] : "in",
        attribute: typeof c["attribute"] === "string" ? c["attribute"] : "",
        values: clauseValues(c["values"]),
        negate: c["negate"] === true,
      })),
      serve: parseServe(r),
    })),
    fallthrough: parseServe(raw["fallthrough"]),
    version: whole(raw["version"]),
  };
}
