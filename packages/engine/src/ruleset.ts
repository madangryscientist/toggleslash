import type { ErrorCode } from "./result.ts";
import {
  type Flag,
  type FlagHeader,
  isFlagBody,
  isFlagHeader,
  isRecord,
  isSegment,
  type Segment,
} from "./validate.ts";

export interface ReadySegment {
  segment: Segment;
  included: ReadonlySet<string>;
  excluded: ReadonlySet<string>;
}

export type FlagEntry =
  | { status: "malformed" }
  | { status: "broken"; header: FlagHeader; code: ErrorCode }
  | { status: "ready"; header: FlagHeader; flag: Flag; targets: ReadonlyMap<string, number> };

export interface Ruleset {
  flags: ReadonlyMap<string, FlagEntry>;
  segments: ReadonlyMap<string, ReadySegment | "malformed">;
}

function indexByKey<T>(items: unknown, compile: (item: unknown) => T): Map<string, T> {
  const index = new Map<string, T>();
  for (const item of Array.isArray(items) ? items : []) {
    const key = isRecord(item) ? item["key"] : undefined;
    if (typeof key === "string" && !index.has(key)) index.set(key, compile(item));
  }
  return index;
}

function compileSegment(value: unknown): ReadySegment | "malformed" {
  if (!isSegment(value)) return "malformed";
  return { segment: value, included: new Set(value.included), excluded: new Set(value.excluded) };
}

/** The first reference to a segment that is missing or malformed, in rule, clause, value order. */
function segmentProblem(flag: Flag, segments: Ruleset["segments"]): ErrorCode | null {
  const clauses = flag.rules.flatMap((rule) => rule.clauses);
  const keys = clauses.flatMap((clause) => (clause.op === "segmentMatch" ? clause.values : []));
  for (const key of keys) {
    const segment = segments.get(key);
    if (segment === undefined) return "SEGMENT_NOT_FOUND";
    if (segment === "malformed") return "MALFORMED_SEGMENT";
  }
  return null;
}

function targetIndex(flag: Flag): Map<string, number> {
  const targets = new Map<string, number>();
  for (const target of flag.targets) {
    for (const contextKey of target.contextKeys) {
      if (!targets.has(contextKey)) targets.set(contextKey, target.variation);
    }
  }
  return targets;
}

function compileFlag(value: unknown, segments: Ruleset["segments"]): FlagEntry {
  if (!isFlagHeader(value)) return { status: "malformed" };
  if (!isFlagBody(value, value.variations.length)) {
    return { status: "broken", header: value, code: "MALFORMED_FLAG" };
  }
  const code = segmentProblem(value, segments);
  if (code !== null) return { status: "broken", header: value, code };
  return { status: "ready", header: value, flag: value, targets: targetIndex(value) };
}

/**
 * Validates and indexes a ruleset (`{ flags, segments }`, as JSON hands it over) once, so each `evaluate` skips that work. It never throws: a bad
 * flag becomes an `ERROR` result when it is evaluated.
 */
export function createRuleset(input: unknown): Ruleset {
  const { flags: rawFlags, segments: rawSegments } = isRecord(input)
    ? input
    : { flags: undefined, segments: undefined };
  const segments = indexByKey(rawSegments, compileSegment);
  const flags = indexByKey(rawFlags, (item) => compileFlag(item, segments));
  return { flags, segments };
}
