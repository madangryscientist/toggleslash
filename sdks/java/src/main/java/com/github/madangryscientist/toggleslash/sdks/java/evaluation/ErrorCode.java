package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The error codes of an {@code ERROR} result, spelled as in the contract. The first seven come from
 * the evaluator; the SDK adds {@link #NOT_READY}, {@link #TYPE_MISMATCH} and {@link
 * #EVALUATION_FAILED}.
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
  EVALUATION_FAILED;

  /** The canonical spelling, as it appears on the wire and in the specification. */
  public String value() {
    return name();
  }

  @Override
  public String toString() {
    return name();
  }
}
