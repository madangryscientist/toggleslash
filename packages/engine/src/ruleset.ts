import { type Contexts, DEFAULT_KIND } from "./context.ts";
import type { ErrorCode } from "./result.ts";
import {
  type ContextList,
  type Flag,
  type FlagHeader,
  isFlagBody,
  isFlagHeader,
  isRecord,
  isSegment,
  type Segment,
} from "./validate.ts";

/** Keys by kind: the kind, then the keys of that kind. */
type KeysByKind = ReadonlyMap<string, ReadonlySet<string>>;

export interface ReadySegment {
  segment: Segment;
  included: KeysByKind;
  excluded: KeysByKind;
}

/** A target that lists a key: its place in `targets` and the variation it serves. */
interface TargetHit {
  order: number;
  variation: number;
}

/** Targets by kind, then by key; the first target in array order holds each key. */
type TargetIndex = ReadonlyMap<string, ReadonlyMap<string, TargetHit>>;

export type FlagEntry =
  | { status: "malformed" }
  | { status: "broken"; header: FlagHeader; code: ErrorCode }
  | { status: "ready"; header: FlagHeader; flag: Flag; targets: TargetIndex };

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

function keysByKind(userKeys: readonly string[], lists: readonly ContextList[] | null | undefined) {
  const index = new Map<string, Set<string>>([[DEFAULT_KIND, new Set(userKeys)]]);
  for (const { contextKind, values } of lists ?? []) {
    const keys = index.get(contextKind) ?? new Set<string>();
    for (const value of values) keys.add(value);
    index.set(contextKind, keys);
  }
  return index;
}

function compileSegment(value: unknown): ReadySegment | "malformed" {
  if (!isSegment(value)) return "malformed";
  return {
    segment: value,
    included: keysByKind(value.included, value.includedContexts),
    excluded: keysByKind(value.excluded, value.excludedContexts),
  };
}

/** Does the context have, in any of its kinds, a key that `keys` lists for that kind? */
export function listsContext(keys: KeysByKind, contexts: Contexts): boolean {
  for (const [kind, context] of contexts) {
    if (keys.get(kind)?.has(context.key) === true) return true;
  }
  return false;
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

function targetIndex(flag: Flag): TargetIndex {
  const index = new Map<string, Map<string, TargetHit>>();
  flag.targets.forEach((target, order) => {
    const kind = target.contextKind ?? DEFAULT_KIND;
    const keys = index.get(kind) ?? new Map<string, TargetHit>();
    for (const contextKey of target.contextKeys) {
      if (!keys.has(contextKey)) keys.set(contextKey, { order, variation: target.variation });
    }
    index.set(kind, keys);
  });
  return index;
}

/** The variation of the first target in array order that matches the context, or undefined. */
export function targetedVariation(targets: TargetIndex, contexts: Contexts): number | undefined {
  let first: TargetHit | undefined;
  for (const [kind, context] of contexts) {
    const hit = targets.get(kind)?.get(context.key);
    if (hit !== undefined && (first === undefined || hit.order < first.order)) first = hit;
  }
  return first?.variation;
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
