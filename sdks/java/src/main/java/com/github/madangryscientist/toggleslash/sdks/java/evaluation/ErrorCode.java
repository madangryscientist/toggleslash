package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * Why an evaluation failed. The first seven come from the evaluator, the last three from the SDK.
 */
public enum ErrorCode {
  /** The context is not valid. */
  INVALID_CONTEXT,
  /** The flag, or a prerequisite, is not in the ruleset. */
  FLAG_NOT_FOUND,
  /** A segment the flag names is not in the ruleset. */
  SEGMENT_NOT_FOUND,
  /** The flag is not valid. */
  MALFORMED_FLAG,
  /** A segment the flag names is not valid. */
  MALFORMED_SEGMENT,
  /** The flag is its own prerequisite, directly or not. */
  PREREQUISITE_CYCLE,
  /** The prerequisite chain is deeper than the limit. */
  PREREQUISITE_DEPTH_EXCEEDED,
  /** The client holds no ruleset yet. */
  NOT_READY,
  /** The served value is not of the kind the accessor asked for. */
  TYPE_MISMATCH,
  /** The evaluator raised an error. */
  EVALUATION_FAILED
}
