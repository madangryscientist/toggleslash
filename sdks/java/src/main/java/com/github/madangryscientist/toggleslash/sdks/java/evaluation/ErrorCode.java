package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** The error codes of the evaluation spec, then the three the SDK adds, in their spelling. */
public enum ErrorCode {
  /** The context is not valid. */
  INVALID_CONTEXT,
  /** The flag, or a prerequisite, does not exist. */
  FLAG_NOT_FOUND,
  /** A segmentMatch names a segment that does not exist. */
  SEGMENT_NOT_FOUND,
  /** A flag does not fit the spec. */
  MALFORMED_FLAG,
  /** A segment a flag refers to does not fit the spec. */
  MALFORMED_SEGMENT,
  /** A flag is its own prerequisite, directly or not. */
  PREREQUISITE_CYCLE,
  /** More than 32 prerequisite links from the requested flag. */
  PREREQUISITE_DEPTH_EXCEEDED,
  /** No ruleset has loaded yet (the SDK's own; the evaluator never gives it). */
  NOT_READY,
  /** The flag served a value of another kind than the fallback (the SDK's own). */
  TYPE_MISMATCH,
  /** The evaluation raised something it should not have (the SDK's own). */
  EVALUATION_FAILED
}
