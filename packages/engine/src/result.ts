export type ErrorCode =
  | "INVALID_CONTEXT"
  | "FLAG_NOT_FOUND"
  | "SEGMENT_NOT_FOUND"
  | "MALFORMED_FLAG"
  | "MALFORMED_SEGMENT"
  | "PREREQUISITE_CYCLE"
  | "PREREQUISITE_DEPTH_EXCEEDED";

export type Reason =
  | { kind: "OFF" }
  | { kind: "TARGET_MATCH" }
  | { kind: "RULE_MATCH"; ruleIndex: number }
  | { kind: "FALLTHROUGH" }
  | { kind: "PREREQUISITE_FAILED"; flagKey: string }
  | { kind: "ERROR"; code: ErrorCode };

export interface EvaluationResult {
  /** The served variation's JSON value; null when nothing is served. */
  value: unknown;
  variationIndex: number | null;
  reason: Reason;
}
