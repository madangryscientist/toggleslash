package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** Why an evaluation gave its answer: the kinds of the evaluation spec, in their spelling. */
public enum ReasonKind {
  /** The flag is off. */
  OFF,
  /** A prerequisite flag was not met. */
  PREREQUISITE_FAILED,
  /** The context is listed in a target. */
  TARGET_MATCH,
  /** A rule matched. */
  RULE_MATCH,
  /** No rule matched, so the fallthrough served. */
  FALLTHROUGH,
  /** The evaluation failed; the error code says how. */
  ERROR
}
