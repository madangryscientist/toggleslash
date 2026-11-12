package com.github.madangryscientist.toggleslash.sdks.java;

/** The code of a {@link ReasonKind#ERROR} reason. {@code toString()} is the constant's name. */
public enum ErrorCode {
  /** The context is not a valid single or multi context. */
  INVALID_CONTEXT,
  /** The ruleset has no such flag. */
  FLAG_NOT_FOUND,
  /** A rule refers to a segment the ruleset does not have. */
  SEGMENT_NOT_FOUND,
  /** The flag is malformed. */
  MALFORMED_FLAG,
  /** A rule refers to a segment that is malformed. */
  MALFORMED_SEGMENT,
  /** Prerequisites lead back to a flag already being evaluated. */
  PREREQUISITE_CYCLE,
  /** Prerequisites nest deeper than the spec allows. */
  PREREQUISITE_DEPTH_EXCEEDED,
  /** No ruleset has been loaded yet. */
  NOT_READY,
  /** The served value is not of the kind the caller asked for. */
  TYPE_MISMATCH,
  /** Evaluating raised an error; the fallback is returned. */
  EVALUATION_FAILED
}
