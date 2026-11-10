/** Extra attributes of one context entry. */
type Attributes = Readonly<Record<string, unknown>> | null;

/** A context of one kind. No `kind` is `"user"`. See `docs/evaluation.md`. */
export interface SingleContext {
  kind?: string | null;
  key: string;
  attributes?: Attributes;
}

/** One entry of a multi context. Its kind is its property name in `contexts`. */
export interface ContextEntry {
  key: string;
  attributes?: Attributes;
}

/** Several contexts of different kinds, evaluated together. */
export interface MultiContext {
  kind: "multi";
  contexts: Readonly<Record<string, ContextEntry>>;
}

export type Context = SingleContext | MultiContext;

/** Why a flag answered what it did, or why the caller got its fallback. */
export type ErrorCode =
  | "INVALID_CONTEXT"
  | "FLAG_NOT_FOUND"
  | "SEGMENT_NOT_FOUND"
  | "MALFORMED_FLAG"
  | "MALFORMED_SEGMENT"
  | "PREREQUISITE_CYCLE"
  | "PREREQUISITE_DEPTH_EXCEEDED"
  | "NOT_READY"
  | "TYPE_MISMATCH"
  | "EVALUATION_FAILED";

export type Reason =
  | { kind: "OFF" }
  | { kind: "TARGET_MATCH" }
  | { kind: "RULE_MATCH"; ruleIndex: number }
  | { kind: "FALLTHROUGH" }
  | { kind: "PREREQUISITE_FAILED"; flagKey: string }
  | { kind: "ERROR"; code: ErrorCode };

export interface VariationDetail<T> {
  /** The served value, or the fallback when nothing usable was served. */
  value: T;
  /** The variation served, or null when `value` is the fallback, a bootstrap value, or the flag serves nothing. */
  variationIndex: number | null;
  reason: Reason;
}

/** One flag's result in the cache. */
export interface Evaluation {
  value: unknown;
  variationIndex: number | null;
  reason: Reason;
}
