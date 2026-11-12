package com.github.madangryscientist.toggleslash.sdks.java;

/** The code of an {@link ReasonKind#ERROR} reason. */
public enum ErrorCode {
  /** The context is not valid. */
  INVALID_CONTEXT,
  /** No such flag. */
  FLAG_NOT_FOUND,
  /** A segment a flag names is missing. */
  SEGMENT_NOT_FOUND,
  /** The flag is malformed. */
  MALFORMED_FLAG,
  /** A segment a flag names is malformed. */
  MALFORMED_SEGMENT,
  /** The prerequisites form a cycle. */
  PREREQUISITE_CYCLE,
  /** The prerequisites are nested too deeply. */
  PREREQUISITE_DEPTH_EXCEEDED,
  /** No ruleset is held yet. */
  NOT_READY,
  /** The served value is not the kind the caller asked for. */
  TYPE_MISMATCH,
  /** The evaluation raised inside the SDK. */
  EVALUATION_FAILED
}
