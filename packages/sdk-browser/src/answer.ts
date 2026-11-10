import type { ErrorCode, Evaluation, Reason } from "./types.ts";

const ERROR_CODES: readonly string[] = [
  "INVALID_CONTEXT",
  "FLAG_NOT_FOUND",
  "SEGMENT_NOT_FOUND",
  "MALFORMED_FLAG",
  "MALFORMED_SEGMENT",
  "PREREQUISITE_CYCLE",
  "PREREQUISITE_DEPTH_EXCEEDED",
  "NOT_READY",
  "TYPE_MISMATCH",
  "EVALUATION_FAILED",
] satisfies ErrorCode[];

function isErrorCode(code: unknown): code is ErrorCode {
  return typeof code === "string" && ERROR_CODES.includes(code);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

const EVALUATION_FAILED: Reason = { kind: "ERROR", code: "EVALUATION_FAILED" };

/**
 * The server's flat `ReasonResponse` (`kind` plus nullable `ruleIndex`, `flagKey` and `code`) as the
 * discriminated union. A kind or a code this SDK does not know becomes `EVALUATION_FAILED`.
 */
function toReason(raw: Record<string, unknown>): Reason {
  const { kind, ruleIndex, flagKey, code } = raw;
  if (kind === "OFF" || kind === "TARGET_MATCH" || kind === "FALLTHROUGH") return { kind };
  if (kind === "RULE_MATCH" && typeof ruleIndex === "number") return { kind, ruleIndex };
  if (kind === "PREREQUISITE_FAILED" && typeof flagKey === "string") return { kind, flagKey };
  if (kind === "ERROR" && isErrorCode(code)) return { kind, code };
  return EVALUATION_FAILED;
}

function toEvaluation(key: string, entry: unknown): Evaluation {
  if (!isRecord(entry) || !("value" in entry) || !isRecord(entry["reason"]))
    throw new Error(`the evaluate answer for "${key}" is malformed`);
  const index = entry["variationIndex"];
  if (index !== null && !(typeof index === "number" && Number.isInteger(index)))
    throw new Error(`the evaluate answer for "${key}" has a bad variationIndex`);
  return { value: entry["value"], variationIndex: index, reason: toReason(entry["reason"]) };
}

/** Reads a `POST /sdk/v1/evaluate` answer. It throws on a shape the server does not send. */
export function readAnswer(text: string): Map<string, Evaluation> {
  const body: unknown = JSON.parse(text);
  const flags = isRecord(body) ? body["flags"] : undefined;
  if (!isRecord(flags)) throw new Error('the evaluate answer has no "flags" object');
  return new Map(Object.entries(flags).map(([key, entry]) => [key, toEvaluation(key, entry)]));
}

function sameJson(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (Array.isArray(a) && Array.isArray(b))
    return a.length === b.length && a.every((item, index) => sameJson(item, b[index]));
  if (!isRecord(a) || !isRecord(b)) return false;
  const keys = Object.keys(a);
  return (
    keys.length === Object.keys(b).length &&
    keys.every((key) => key in b && sameJson(a[key], b[key]))
  );
}

/** The sorted keys whose value or variation index changed, appeared or disappeared. */
export function changedKeys(
  before: ReadonlyMap<string, Evaluation>,
  after: ReadonlyMap<string, Evaluation>,
): string[] {
  const changed: string[] = [];
  for (const [key, next] of after) {
    const previous = before.get(key);
    if (
      !previous ||
      previous.variationIndex !== next.variationIndex ||
      !sameJson(previous.value, next.value)
    )
      changed.push(key);
  }
  for (const key of before.keys()) if (!after.has(key)) changed.push(key);
  return changed.toSorted();
}
