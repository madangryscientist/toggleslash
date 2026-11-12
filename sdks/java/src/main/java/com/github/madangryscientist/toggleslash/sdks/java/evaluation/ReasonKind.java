package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** Why a flag evaluated the way it did. {@link #name()} is the wire name. */
public enum ReasonKind {
  /** The flag is off. */
  OFF,
  /** A target lists the context key. */
  TARGET_MATCH,
  /** A rule matched. */
  RULE_MATCH,
  /** No rule matched, so the fallthrough served. */
  FALLTHROUGH,
  /** A prerequisite was not met. */
  PREREQUISITE_FAILED,
  /** The evaluation failed; see {@link ErrorCode}. */
  ERROR
}
