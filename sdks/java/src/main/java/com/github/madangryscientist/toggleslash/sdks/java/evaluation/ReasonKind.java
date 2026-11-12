package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** Why a flag evaluated to what it did. The constant names are the canonical spelling. */
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
