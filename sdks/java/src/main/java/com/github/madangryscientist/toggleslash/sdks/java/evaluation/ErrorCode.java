package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The error codes: the seven the evaluator returns and the three the client adds. {@link #name()}
 * is the wire name.
 */
public enum ErrorCode {
  /** The context is not valid. */
  INVALID_CONTEXT,
  /** The flag is not in the ruleset. */
  FLAG_NOT_FOUND,
  /** A segment the flag refers to is not in the ruleset. */
  SEGMENT_NOT_FOUND,
  /** The flag is not valid. */
  MALFORMED_FLAG,
  /** A segment the flag refers to is not valid. */
  MALFORMED_SEGMENT,
  /** Prerequisites lead back to a flag being evaluated. */
  PREREQUISITE_CYCLE,
  /** Prerequisites nest deeper than 32. */
  PREREQUISITE_DEPTH_EXCEEDED,
  /** The client has no ruleset yet. */
  NOT_READY,
  /** The flag's value is not of the kind the accessor asked for. */
  TYPE_MISMATCH,
  /** Evaluating raised an error. */
  EVALUATION_FAILED
}
