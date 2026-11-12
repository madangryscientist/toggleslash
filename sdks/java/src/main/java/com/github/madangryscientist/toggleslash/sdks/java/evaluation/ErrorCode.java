package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The error codes of an {@code ERROR} reason. The constant names are the wire spelling, so {@link
 * #name()} is what the SDK reports and what a JSON answer carries.
 */
public enum ErrorCode {
  /** The context is not valid. */
  INVALID_CONTEXT,
  /** No flag has the key. */
  FLAG_NOT_FOUND,
  /** A flag names a segment that the ruleset does not have. */
  SEGMENT_NOT_FOUND,
  /** The flag is not valid. */
  MALFORMED_FLAG,
  /** A segment the flag names is not valid. */
  MALFORMED_SEGMENT,
  /** Prerequisites lead back to a flag being evaluated. */
  PREREQUISITE_CYCLE,
  /** Prerequisites go more than 32 links deep. */
  PREREQUISITE_DEPTH_EXCEEDED,
  /** The SDK holds no ruleset yet. */
  NOT_READY,
  /** The served value is not of the kind the accessor reads. */
  TYPE_MISMATCH,
  /** The evaluator raised. */
  EVALUATION_FAILED
}
