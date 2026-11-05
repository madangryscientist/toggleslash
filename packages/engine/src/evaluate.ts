import { bucket } from "./bucket.ts";
import { attributeClauseMatches, type Context } from "./clause.ts";
import type { ErrorCode, EvaluationResult, Reason } from "./result.ts";
import type { FlagEntry, ReadySegment, Ruleset } from "./ruleset.ts";
import {
  type Clause,
  type Flag,
  type FlagHeader,
  isRecord,
  type Prerequisite,
  type Rollout,
  type Serve,
  type SegmentRule,
} from "./validate.ts";

/** Prerequisite links from the requested flag; docs/evaluation.md pins the limit. */
const MAX_PREREQUISITE_DEPTH = 32;

interface Run {
  ruleset: Ruleset;
  context: Context;
  active: Set<string>;
  completed: Map<string, EvaluationResult>;
}

function failure(code: ErrorCode): EvaluationResult {
  return { value: null, variationIndex: null, reason: { kind: "ERROR", code } };
}

function served(variations: readonly unknown[], index: number, reason: Reason): EvaluationResult {
  return { value: variations[index] ?? null, variationIndex: index, reason };
}

function offResult(header: FlagHeader, reason: Reason): EvaluationResult {
  const index = header.offVariation;
  if (index === null || index === undefined) return { value: null, variationIndex: null, reason };
  return served(header.variations, index, reason);
}

function rolloutVariation(rollout: Rollout, position: number): number {
  let chosen = 0;
  let cumulative = 0;
  for (const { variation, weight } of rollout.variations) {
    chosen = variation;
    cumulative += weight;
    if (position < cumulative) break;
  }
  return chosen;
}

function serveResult(flag: Flag, serve: Serve, contextKey: string, reason: Reason) {
  const index = serve.rollout
    ? rolloutVariation(serve.rollout, bucket(flag.key, flag.salt, contextKey))
    : serve.variation;
  return served(flag.variations, index, reason);
}

function segmentRuleMatches(rule: SegmentRule, ready: ReadySegment, context: Context): boolean {
  if (!rule.clauses.every((clause) => attributeClauseMatches(clause, context))) return false;
  if (rule.weight === null || rule.weight === undefined) return true;
  return bucket(ready.segment.key, ready.segment.salt, context.key) < rule.weight;
}

function segmentMatches(ready: ReadySegment, context: Context): boolean {
  if (ready.excluded.has(context.key)) return false;
  if (ready.included.has(context.key)) return true;
  return ready.segment.rules.some((rule) => segmentRuleMatches(rule, ready, context));
}

function clauseMatches(clause: Clause, run: Run): boolean {
  if (clause.op !== "segmentMatch") return attributeClauseMatches(clause, run.context);
  const matched = clause.values.some((key) => {
    const ready = run.ruleset.segments.get(key);
    return ready !== undefined && ready !== "malformed" && segmentMatches(ready, run.context);
  });
  return clause.negate === true ? !matched : matched;
}

function prerequisiteMet(prerequisite: Prerequisite, result: EvaluationResult): boolean {
  const kind = result.reason.kind;
  const reachedServe = kind === "TARGET_MATCH" || kind === "RULE_MATCH" || kind === "FALLTHROUGH";
  return reachedServe && result.variationIndex === prerequisite.variation;
}

function checkPrerequisites(flag: Flag, run: Run, depth: number): EvaluationResult | null {
  for (const prerequisite of flag.prerequisites) {
    const result = evaluateFlag(run, prerequisite.flagKey, depth + 1);
    if (result.reason.kind === "ERROR") return result;
    if (!prerequisiteMet(prerequisite, result)) {
      return offResult(flag, { kind: "PREREQUISITE_FAILED", flagKey: prerequisite.flagKey });
    }
  }
  return null;
}

function evaluateEntry(entry: FlagEntry, run: Run, depth: number): EvaluationResult {
  if (entry.status === "malformed") return failure("MALFORMED_FLAG");
  if (!entry.header.on) return offResult(entry.header, { kind: "OFF" });
  if (entry.status === "broken") return failure(entry.code);
  const { flag, targets } = entry;
  const blocked = checkPrerequisites(flag, run, depth);
  if (blocked) return blocked;
  const targeted = targets.get(run.context.key);
  if (targeted !== undefined) return served(flag.variations, targeted, { kind: "TARGET_MATCH" });
  const ruleIndex = flag.rules.findIndex((rule) =>
    rule.clauses.every((clause) => clauseMatches(clause, run)),
  );
  const rule = flag.rules[ruleIndex];
  if (rule) return serveResult(flag, rule, run.context.key, { kind: "RULE_MATCH", ruleIndex });
  return serveResult(flag, flag.fallthrough, run.context.key, { kind: "FALLTHROUGH" });
}

function evaluateFlag(run: Run, key: string, depth: number): EvaluationResult {
  const completed = run.completed.get(key);
  if (completed) return completed;
  if (run.active.has(key)) return failure("PREREQUISITE_CYCLE");
  if (depth > MAX_PREREQUISITE_DEPTH) return failure("PREREQUISITE_DEPTH_EXCEEDED");
  const entry = run.ruleset.flags.get(key);
  if (!entry) return failure("FLAG_NOT_FOUND");
  run.active.add(key);
  const result = evaluateEntry(entry, run, depth);
  run.active.delete(key);
  run.completed.set(key, result);
  return result;
}

function isContext(value: unknown): value is Context {
  if (!isRecord(value) || typeof value["key"] !== "string" || value["key"] === "") return false;
  const attributes = value["attributes"];
  return attributes === undefined || attributes === null || isRecord(attributes);
}

/**
 * Evaluates one flag for one context. It never throws: every failure, including a prerequisite
 * cycle, is an `ERROR` result. The order and the comparison rules are in docs/evaluation.md.
 */
export function evaluate(ruleset: Ruleset, flagKey: string, context: Context): EvaluationResult {
  if (!isContext(context)) return failure("INVALID_CONTEXT");
  return evaluateFlag({ ruleset, context, active: new Set(), completed: new Map() }, flagKey, 0);
}
