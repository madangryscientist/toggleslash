package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The codes of an {@code ERROR} reason: the evaluator's seven, then the three the SDK adds
 * (contract section 4). The constant names are the canonical spelling.
 */
public enum ErrorCode {
  /** The context is not valid. */
  INVALID_CONTEXT,
  /** The requested flag, or a prerequisite, does not exist. */
  FLAG_NOT_FOUND,
  /** A {@code segmentMatch} names a segment that does not exist. */
  SEGMENT_NOT_FOUND,
  /** A flag does not fit the evaluation spec. */
  MALFORMED_FLAG,
  /** A segment a flag refers to does not fit the evaluation spec. */
  MALFORMED_SEGMENT,
  /** A flag is its own prerequisite, directly or not. */
  PREREQUISITE_CYCLE,
  /** More than 32 prerequisite links from the requested flag. */
  PREREQUISITE_DEPTH_EXCEEDED,
  /** The SDK holds no ruleset yet. */
  NOT_READY,
  /** The flag served a value of a different kind than the fallback. */
  TYPE_MISMATCH,
  /** The evaluation raised something it should not have. */
  EVALUATION_FAILED
}
