package com.github.madangryscientist.toggleslash.sdks.java;

/** Why an evaluation answered what it did. */
public enum ReasonKind {
  /** The flag is off. */
  OFF,
  /** A prerequisite was not met. */
  PREREQUISITE_FAILED,
  /** A target listed the context. */
  TARGET_MATCH,
  /** A rule matched. */
  RULE_MATCH,
  /** No target or rule matched. */
  FALLTHROUGH,
  /** The evaluation failed; see {@link ErrorCode}. */
  ERROR
}
