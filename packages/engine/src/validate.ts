import { BUCKET_COUNT } from "./bucket.ts";
import { parseReference } from "./reference.ts";

/**
 * The shape of a ruleset, checked at runtime because rulesets arrive as JSON. `docs/evaluation.md`
 * is normative for what counts as malformed. Null and absent are the same for an optional field.
 */
export type ClauseValue = string | number | boolean;
export type AttributeOp =
  | "in"
  | "contains"
  | "startsWith"
  | "endsWith"
  | "lt"
  | "lte"
  | "gt"
  | "gte"
  | "semverEq"
  | "semverLt"
  | "semverLte"
  | "semverGt"
  | "semverGte"
  | "before"
  | "after"
  | "matches";

export interface AttributeClause {
  op: AttributeOp;
  attribute: string;
  values: readonly ClauseValue[];
  negate?: boolean | null;
  contextKind?: string | null;
}
interface SegmentClause {
  op: "segmentMatch";
  values: readonly string[];
  negate?: boolean | null;
}
export type Clause = AttributeClause | SegmentClause;

interface WeightedVariation {
  variation: number;
  weight: number;
}
export interface Rollout {
  variations: readonly WeightedVariation[];
  contextKind?: string | null;
}
export type Serve = { variation: number; rollout?: null } | { rollout: Rollout; variation?: null };
type Rule = Serve & { clauses: readonly Clause[] };

interface Target {
  variation: number;
  contextKeys: readonly string[];
  contextKind?: string | null;
}
export interface Prerequisite {
  flagKey: string;
  variation: number;
}
export interface FlagHeader {
  key: string;
  on: boolean;
  variations: readonly unknown[];
  offVariation?: number | null;
}
interface FlagBody {
  salt: string;
  version: number;
  targets: readonly Target[];
  prerequisites: readonly Prerequisite[];
  rules: readonly Rule[];
  fallthrough: Serve;
}
export type Flag = FlagHeader & FlagBody;

export interface SegmentRule {
  clauses: readonly AttributeClause[];
  weight?: number | null;
  rolloutContextKind?: string | null;
}
/** The keys of one kind that a segment includes or excludes. */
export interface ContextList {
  contextKind: string;
  values: readonly string[];
}
export interface Segment {
  key: string;
  salt: string;
  included: readonly string[];
  excluded: readonly string[];
  includedContexts?: readonly ContextList[] | null;
  excludedContexts?: readonly ContextList[] | null;
  rules: readonly SegmentRule[];
}

const ATTRIBUTE_OPS: ReadonlySet<unknown> = new Set<AttributeOp>([
  "in",
  "contains",
  "startsWith",
  "endsWith",
  "lt",
  "lte",
  "gt",
  "gte",
  "semverEq",
  "semverLt",
  "semverLte",
  "semverGt",
  "semverGte",
  "before",
  "after",
  "matches",
]);

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function isAbsent(value: unknown): value is null | undefined {
  return value === null || value === undefined;
}

// ASCII only, and `$` in JavaScript is the end of the input; the C# side walks the characters.
const KIND_PATTERN = /^[A-Za-z0-9._-]+$/;

/** `^[A-Za-z0-9._-]+$`, and neither `kind` nor `multi`. */
export function isKind(value: unknown): value is string {
  return (
    typeof value === "string" && KIND_PATTERN.test(value) && value !== "kind" && value !== "multi"
  );
}

function isOptionalKind(value: unknown): boolean {
  return isAbsent(value) || isKind(value);
}

function isKey(value: unknown): value is string {
  return typeof value === "string" && value !== "";
}

function isCount(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

function isVariationIndex(value: unknown, variationCount: number): value is number {
  return isCount(value) && value < variationCount;
}

function isStringArray(value: unknown): value is readonly string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}

function isClauseValue(value: unknown): value is ClauseValue {
  if (typeof value === "number") return Number.isFinite(value);
  return typeof value === "string" || typeof value === "boolean";
}

function isNegate(value: unknown): value is boolean | null | undefined {
  return isAbsent(value) || typeof value === "boolean";
}

function isAttributeName(value: unknown): value is string {
  if (!isKey(value)) return false;
  return !value.startsWith("/") || parseReference(value) !== null;
}

function isAttributeClause(value: unknown): value is AttributeClause {
  if (!isRecord(value) || !ATTRIBUTE_OPS.has(value["op"])) return false;
  const values = value["values"];
  return (
    isAttributeName(value["attribute"]) &&
    isNegate(value["negate"]) &&
    isOptionalKind(value["contextKind"]) &&
    Array.isArray(values) &&
    values.every(isClauseValue)
  );
}

function isSegmentClause(value: unknown): value is SegmentClause {
  if (!isRecord(value) || value["op"] !== "segmentMatch") return false;
  return isNegate(value["negate"]) && isStringArray(value["values"]);
}

function isWeightedVariation(value: unknown, variationCount: number): value is WeightedVariation {
  if (!isRecord(value)) return false;
  const weight = value["weight"];
  return (
    isVariationIndex(value["variation"], variationCount) &&
    isCount(weight) &&
    weight <= BUCKET_COUNT
  );
}

function isRollout(value: unknown, variationCount: number): value is Rollout {
  if (!isRecord(value) || !isOptionalKind(value["contextKind"])) return false;
  const variations: unknown = value["variations"];
  if (!Array.isArray(variations) || variations.length === 0) return false;
  let total = 0;
  for (const item of variations) {
    if (!isWeightedVariation(item, variationCount)) return false;
    total += item.weight;
  }
  return total === BUCKET_COUNT;
}

function isServe(value: unknown, variationCount: number): value is Serve {
  if (!isRecord(value)) return false;
  const variation = value["variation"];
  const rollout = value["rollout"];
  if (isAbsent(rollout)) return isVariationIndex(variation, variationCount);
  return isAbsent(variation) && isRollout(rollout, variationCount);
}

function isRule(value: unknown, variationCount: number): value is Rule {
  if (!isRecord(value)) return false;
  const clauses: unknown = value["clauses"];
  return (
    Array.isArray(clauses) &&
    clauses.every((clause) => isAttributeClause(clause) || isSegmentClause(clause)) &&
    isServe(value, variationCount)
  );
}

function isTarget(value: unknown, variationCount: number): value is Target {
  if (!isRecord(value)) return false;
  return (
    isVariationIndex(value["variation"], variationCount) &&
    isStringArray(value["contextKeys"]) &&
    isOptionalKind(value["contextKind"])
  );
}

function isPrerequisite(value: unknown): value is Prerequisite {
  return isRecord(value) && isKey(value["flagKey"]) && isCount(value["variation"]);
}

/** What serving the off variation needs. It is checked apart so `on: false` survives a bad rule. */
export function isFlagHeader(value: unknown): value is FlagHeader {
  if (!isRecord(value) || typeof value["on"] !== "boolean") return false;
  const variations: unknown = value["variations"];
  if (!isKey(value["key"]) || !Array.isArray(variations) || variations.length === 0) return false;
  const offVariation = value["offVariation"];
  return isAbsent(offVariation) || isVariationIndex(offVariation, variations.length);
}

function everyItem(value: unknown, check: (item: unknown) => boolean): boolean {
  return Array.isArray(value) && value.every((item) => check(item));
}

export function isFlagBody(value: unknown, variationCount: number): value is FlagBody {
  if (!isRecord(value)) return false;
  return (
    typeof value["salt"] === "string" &&
    isCount(value["version"]) &&
    everyItem(value["targets"], (item) => isTarget(item, variationCount)) &&
    everyItem(value["prerequisites"], isPrerequisite) &&
    everyItem(value["rules"], (item) => isRule(item, variationCount)) &&
    isServe(value["fallthrough"], variationCount)
  );
}

function isSegmentRule(value: unknown): value is SegmentRule {
  if (!isRecord(value)) return false;
  const weight = value["weight"];
  const weightIsValid = isAbsent(weight) || (isCount(weight) && weight <= BUCKET_COUNT);
  return (
    weightIsValid &&
    isOptionalKind(value["rolloutContextKind"]) &&
    everyItem(value["clauses"], isAttributeClause)
  );
}

function isContextList(value: unknown): value is ContextList {
  return isRecord(value) && isKind(value["contextKind"]) && isStringArray(value["values"]);
}

function isOptionalContextLists(value: unknown): boolean {
  return isAbsent(value) || everyItem(value, isContextList);
}

export function isSegment(value: unknown): value is Segment {
  if (!isRecord(value)) return false;
  return (
    isKey(value["key"]) &&
    typeof value["salt"] === "string" &&
    isStringArray(value["included"]) &&
    isStringArray(value["excluded"]) &&
    isOptionalContextLists(value["includedContexts"]) &&
    isOptionalContextLists(value["excludedContexts"]) &&
    everyItem(value["rules"], isSegmentRule)
  );
}
