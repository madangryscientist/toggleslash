import { toInstant } from "./date.ts";
import { parseReference } from "./reference.ts";
import { compareSemanticVersions, parseSemanticVersion } from "./semver.ts";
import { isRecord } from "./validate.ts";
import type { AttributeClause, AttributeOp, ClauseValue } from "./validate.ts";

export interface Context {
  key: string;
  attributes?: Readonly<Record<string, unknown>> | null;
}

function ownValue(source: Readonly<Record<string, unknown>>, name: string): unknown {
  return Object.hasOwn(source, name) ? source[name] : undefined;
}

/** The value a name or a `/` reference reaches, or undefined when it is missing. */
function readAttribute(context: Context, name: string): unknown {
  const components = name.startsWith("/") ? parseReference(name) : [name];
  const [first, ...rest] = components ?? [];
  if (first === undefined) return undefined;
  const attributes = context.attributes;
  let current: unknown = undefined;
  if (first === "key") current = context.key;
  else if (attributes) current = ownValue(attributes, first);
  for (const component of rest) {
    if (!isRecord(current)) return undefined;
    current = ownValue(current, component);
  }
  return current;
}

function sign<T extends string | number>(a: T, b: T): number {
  if (a < b) return -1;
  return a > b ? 1 : 0;
}

/** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise null. */
function compare(attribute: unknown, value: ClauseValue): number | null {
  if (typeof attribute === "number" && typeof value === "number") {
    return Number.isFinite(attribute) ? sign(attribute, value) : null;
  }
  if (typeof attribute === "string" && typeof value === "string") return sign(attribute, value);
  return null;
}

function matchesString(op: AttributeOp, attribute: string, value: string): boolean {
  if (op === "contains") return attribute.includes(value);
  if (op === "startsWith") return attribute.startsWith(value);
  return attribute.endsWith(value);
}

function matchesOrder(op: AttributeOp, order: number): boolean {
  if (op === "lt") return order < 0;
  if (op === "lte") return order <= 0;
  if (op === "gt") return order > 0;
  return order >= 0;
}

function matchesSemver(op: AttributeOp, attribute: unknown, value: ClauseValue): boolean {
  if (typeof attribute !== "string" || typeof value !== "string") return false;
  const candidate = parseSemanticVersion(attribute);
  const target = parseSemanticVersion(value);
  if (candidate === null || target === null) return false;
  const order = compareSemanticVersions(candidate, target);
  if (op === "semverEq") return order === 0;
  if (op === "semverLt") return order < 0;
  if (op === "semverLte") return order <= 0;
  return op === "semverGt" ? order > 0 : order >= 0;
}

function matchesDate(op: AttributeOp, attribute: unknown, value: ClauseValue): boolean {
  const candidate = toInstant(attribute);
  const target = toInstant(value);
  if (candidate === null || target === null) return false;
  return op === "before" ? candidate < target : candidate > target;
}

const TYPED_OPS: ReadonlySet<AttributeOp> = new Set<AttributeOp>([
  "semverEq",
  "semverLt",
  "semverLte",
  "semverGt",
  "semverGte",
  "before",
  "after",
]);

/** The ops that read a candidate and a value as a version or as an instant. */
function matchesTyped(op: AttributeOp, attribute: unknown, value: ClauseValue): boolean {
  return op === "before" || op === "after"
    ? matchesDate(op, attribute, value)
    : matchesSemver(op, attribute, value);
}

function matchesValue(op: AttributeOp, attribute: unknown, value: ClauseValue): boolean {
  if (op === "in") {
    return typeof attribute === "boolean" ? attribute === value : compare(attribute, value) === 0;
  }
  if (op === "contains" || op === "startsWith" || op === "endsWith") {
    return (
      typeof attribute === "string" &&
      typeof value === "string" &&
      matchesString(op, attribute, value)
    );
  }
  if (TYPED_OPS.has(op)) return matchesTyped(op, attribute, value);
  const order = compare(attribute, value);
  return order !== null && matchesOrder(op, order);
}

/** Does one attribute clause hold? A missing attribute fails it, whatever `negate` says. */
export function attributeClauseMatches(clause: AttributeClause, context: Context): boolean {
  const attribute = readAttribute(context, clause.attribute);
  if (attribute === undefined || attribute === null) return false;
  const candidates: readonly unknown[] = Array.isArray(attribute) ? attribute : [attribute];
  const matched = candidates.some((candidate) =>
    clause.values.some((value) => matchesValue(clause.op, candidate, value)),
  );
  return clause.negate === true ? !matched : matched;
}
