package com.github.madangryscientist.toggleslash.sdks.java;

/** The kind of a {@link Reason} (contract section 2). */
public enum ReasonKind {
  /** The flag is off. */
  OFF,
  /** A prerequisite of the flag was not met. */
  PREREQUISITE_FAILED,
  /** A target listed the context. */
  TARGET_MATCH,
  /** A rule matched. */
  RULE_MATCH,
  /** No target or rule matched. */
  FALLTHROUGH,
  /** The evaluation failed; the reason carries the {@link ErrorCode}. */
  ERROR
}
