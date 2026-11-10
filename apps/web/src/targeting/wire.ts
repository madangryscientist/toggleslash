/** The PUT body the editor builds from a draft, and the shape check on it when it comes back. Pure. */
import { isRecord } from "../api/json.ts";
import { SEGMENT_OP, SEMVER_OPS } from "./model.ts";
import { weightOf, type ClauseDraft, type Draft, type ServeDraft } from "./draft.ts";
import type { FieldProblem } from "./problems.ts";
import { isValidKind, parseKeys, parseStrings, parseValues } from "./values.ts";

export interface Wire {
  on: boolean;
  offVariation: number | null;
  targets: unknown[];
  prerequisites: unknown[];
  rules: unknown[];
  fallthrough: unknown;
}

function list(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}

/** A wire body read back from the browser: the shape only, since the server judges the contents. */
export function wireOf(raw: unknown): Wire {
  const d = isRecord(raw) ? raw : {};
  return {
    on: d["on"] === true,
    offVariation: typeof d["offVariation"] === "number" ? d["offVariation"] : null,
    targets: list(d["targets"]),
    prerequisites: list(d["prerequisites"]),
    rules: list(d["rules"]),
    fallthrough: isRecord(d["fallthrough"]) ? d["fallthrough"] : {},
  };
}

const KIND_MESSAGE =
  "A context kind uses letters, digits, . _ and -, and is not kind or multi. Leave it blank for user.";

/**
 * `{ contextKind }` for a clause or rollout: nothing for blank or `user` (the same thing to the server),
 * a problem for a kind that is not valid.
 */
function kindWire(kind: string, path: string, problems: FieldProblem[]): { contextKind?: string } {
  const text = kind.trim();
  if (text === "" || text === "user") return {};
  if (!isValidKind(text)) {
    problems.push({ path, message: KIND_MESSAGE });
    return {};
  }
  return { contextKind: text };
}

function serveWire(
  serve: ServeDraft,
  path: string,
  problems: FieldProblem[],
): Record<string, unknown> {
  if (serve.kind === "variation") return { variation: serve.variation };
  const variations: { variation: number; weight: number }[] = [];
  serve.entries.forEach((entry, i) => {
    const weight = weightOf(entry.percent);
    if (weight === null) {
      problems.push({
        path: `${path}.rollout.entries[${i}]`,
        message: "A percentage from 0 to 100, with up to three decimals.",
      });
      return;
    }
    // A zero-weight entry buckets nobody, so it is left out rather than sent.
    if (weight > 0) variations.push({ variation: entry.variation, weight });
  });
  return {
    rollout: {
      variations,
      ...kindWire(serve.contextKind, `${path}.rollout.contextKind`, problems),
    },
  };
}

function isSemverOp(op: string): boolean {
  return SEMVER_OPS.some((semver) => semver === op);
}

function clauseWire(
  c: ClauseDraft,
  path: string,
  problems: FieldProblem[],
): Record<string, unknown> {
  const negate = c.negate ? { negate: true } : {};
  if (c.op === SEGMENT_OP) return { op: c.op, values: parseStrings(c.values), ...negate };
  // A version is a string even when it looks like a number: a bare `2.1` must not be sent as 2.1.
  const values = isSemverOp(c.op) ? parseStrings(c.values) : parseValues(c.values);
  return {
    op: c.op,
    attribute: c.attribute.trim(),
    values,
    ...negate,
    ...kindWire(c.contextKind, `${path}.contextKind`, problems),
  };
}

/** An other-kind target must name a kind that is valid and not `user`; a user target (`undefined`) sends none. */
function targetKind(
  kind: string | undefined,
  path: string,
  problems: FieldProblem[],
): { contextKind?: string } {
  if (kind === undefined) return {};
  const text = kind.trim();
  if (text === "user" || !isValidKind(text)) {
    problems.push({ path, message: KIND_MESSAGE });
    return {};
  }
  return { contextKind: text };
}

/** The body of a PUT, or the fields a person must fix first (the rest is the server's to judge). */
export function toWire(
  draft: Draft,
): { ok: true; wire: Wire } | { ok: false; problems: FieldProblem[] } {
  const problems: FieldProblem[] = [];
  const wire: Wire = {
    on: draft.on,
    offVariation: draft.offVariation,
    targets: draft.targets.map((t, i) => ({
      variation: t.variation,
      contextKeys: parseKeys(t.keys),
      ...targetKind(t.contextKind, `targets[${i}].contextKind`, problems),
    })),
    prerequisites: draft.prerequisites.map((p) => ({ flagKey: p.flagKey, variation: p.variation })),
    rules: draft.rules.map((r, i) => ({
      clauses: r.clauses.map((c, j) => clauseWire(c, `rules[${i}].clauses[${j}]`, problems)),
      ...serveWire(r.serve, `rules[${i}]`, problems),
    })),
    fallthrough: serveWire(draft.fallthrough, "fallthrough", problems),
  };
  return problems.length > 0 ? { ok: false, problems } : { ok: true, wire };
}
