package com.github.madangryscientist.toggleslash.sdks.java;

/** Why an evaluation ended in an {@code ERROR} reason (contract section 2). */
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
  /** The client has no ruleset yet. */
  NOT_READY,
  /** The value is not of the type the accessor asked for. */
  TYPE_MISMATCH,
  /** The evaluation itself failed unexpectedly. */
  EVALUATION_FAILED
}
