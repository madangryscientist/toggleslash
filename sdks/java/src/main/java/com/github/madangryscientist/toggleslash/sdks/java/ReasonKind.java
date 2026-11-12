package com.github.madangryscientist.toggleslash.sdks.java;

/** Why a flag answered as it did: the {@code kind} of a {@link Reason}. */
public enum ReasonKind {
  /** The flag is off. */
  OFF,
  /** A prerequisite flag did not serve the variation the flag needs. */
  PREREQUISITE_FAILED,
  /** The context is in a target list. */
  TARGET_MATCH,
  /** A rule matched. */
  RULE_MATCH,
  /** No target or rule matched, so the fallthrough served. */
  FALLTHROUGH,
  /** The answer is the fallback because of an {@link ErrorCode}. */
  ERROR
}
