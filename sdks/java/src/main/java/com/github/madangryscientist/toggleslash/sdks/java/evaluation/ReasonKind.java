package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The kinds of reason an evaluation answers with. The constant names are the wire spelling, so
 * {@link #name()} is what the SDK reports.
 */
public enum ReasonKind {
  /** The flag is off. */
  OFF,
  /** A target lists the context. */
  TARGET_MATCH,
  /** A rule matched. */
  RULE_MATCH,
  /** No target or rule matched. */
  FALLTHROUGH,
  /** A prerequisite flag did not serve the variation required. */
  PREREQUISITE_FAILED,
  /** The evaluation failed; see {@link ErrorCode}. */
  ERROR
}
