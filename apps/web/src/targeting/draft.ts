/**
 * A config as a person edits it: the strings they type (a percentage, a comma-separated value list)
 * instead of the numbers and arrays the server stores, so a half-typed field is never lost. Pure.
 */
import type { Config, Prerequisite, Serve } from "./model.ts";
import { WEIGHT_TOTAL } from "./model.ts";
import { formatValues } from "./values.ts";

export interface ClauseDraft {
  op: string;
  attribute: string;
  values: string;
  negate: boolean;
}

export interface RolloutDraft {
  variation: number;
  /** A percentage as typed: `"25"`, `"33.333"`. */
  percent: string;
}

export type ServeDraft =
  | { kind: "variation"; variation: number }
  | { kind: "rollout"; entries: RolloutDraft[] };

export interface RuleDraft {
  clauses: ClauseDraft[];
  serve: ServeDraft;
}

export interface TargetDraft {
  variation: number;
  /** One context key per line. */
  keys: string;
}

export interface Draft {
  on: boolean;
  offVariation: number | null;
  targets: TargetDraft[];
  prerequisites: Prerequisite[];
  rules: RuleDraft[];
  fallthrough: ServeDraft;
}

/** `25000` as `"25"`, `33333` as `"33.333"`. */
export function percentOf(weight: number): string {
  return String(weight / 1000);
}

/** `"25"` as 25000; `null` when it is not a number from 0 to 100 with at most three decimals. */
export function weightOf(percent: string): number | null {
  const text = percent.trim();
  if (!/^\d+(\.\d{1,3})?$/.test(text)) return null;
  const weight = Math.round(Number(text) * 1000);
  return weight <= WEIGHT_TOTAL ? weight : null;
}

function serveDraft(serve: Serve, variationCount: number): ServeDraft {
  if (serve.kind === "variation") return serve;
  const entries = serve.entries.map((e) => ({
    variation: e.variation,
    percent: percentOf(e.weight),
  }));
  // Every variation gets a line so a percentage can be given to it; absent ones start at 0.
  for (let v = 0; v < variationCount; v++) {
    if (!entries.some((e) => e.variation === v)) entries.push({ variation: v, percent: "0" });
  }
  return { kind: "rollout", entries };
}

export function toDraft(config: Config, variationCount: number): Draft {
  return {
    on: config.on,
    offVariation: config.offVariation,
    targets: config.targets.map((t) => ({
      variation: t.variation,
      keys: t.contextKeys.join("\n"),
    })),
    prerequisites: config.prerequisites.map((p) => ({ ...p })),
    rules: config.rules.map((r) => ({
      clauses: r.clauses.map((c) => ({
        op: c.op,
        attribute: c.attribute,
        values: formatValues(c.values),
        negate: c.negate,
      })),
      serve: serveDraft(r.serve, variationCount),
    })),
    fallthrough: serveDraft(config.fallthrough, variationCount),
  };
}

/** The same serve as a draft, switched between one variation and a rollout. */
export function switchServe(
  serve: ServeDraft,
  to: "variation" | "rollout",
  count: number,
): ServeDraft {
  if (serve.kind === "variation") {
    if (to === "variation") return serve;
    return {
      kind: "rollout",
      entries: Array.from({ length: count }, (_, v) => ({
        variation: v,
        percent: v === serve.variation ? "100" : "0",
      })),
    };
  }
  if (to === "rollout") return serve;
  const biggest = serve.entries.reduce<RolloutDraft | null>(
    (best, e) => (best === null || Number(e.percent) > Number(best.percent) ? e : best),
    null,
  );
  return { kind: "variation", variation: biggest?.variation ?? 0 };
}

export function emptyClause(): ClauseDraft {
  return { op: "in", attribute: "", values: "", negate: false };
}

export function isDirty(a: Draft, b: Draft): boolean {
  return JSON.stringify(a) !== JSON.stringify(b);
}

export function rolloutTotal(entries: RolloutDraft[]): number {
  let total = 0;
  for (const e of entries) total += weightOf(e.percent) ?? 0;
  return total;
}
