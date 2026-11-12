package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** The kind of a {@link Reason}. */
public enum ReasonKind {
  /** The flag is off. */
  OFF,
  /** A target lists the context. */
  TARGET_MATCH,
  /** A rule matched. */
  RULE_MATCH,
  /** No rule matched, so the fallthrough served. */
  FALLTHROUGH,
  /** A prerequisite was not met. */
  PREREQUISITE_FAILED,
  /** The evaluation failed. */
  ERROR
}
