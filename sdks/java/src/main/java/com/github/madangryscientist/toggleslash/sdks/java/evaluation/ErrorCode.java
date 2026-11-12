package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The error codes of {@code ERROR} reasons. The first seven come from the evaluator, the rest from
 * the SDK.
 */
public enum ErrorCode {
  INVALID_CONTEXT,
  FLAG_NOT_FOUND,
  SEGMENT_NOT_FOUND,
  MALFORMED_FLAG,
  MALFORMED_SEGMENT,
  PREREQUISITE_CYCLE,
  PREREQUISITE_DEPTH_EXCEEDED,
  NOT_READY,
  TYPE_MISMATCH,
  EVALUATION_FAILED
}
