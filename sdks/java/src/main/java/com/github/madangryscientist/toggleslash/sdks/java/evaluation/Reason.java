package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.OptionalInt;

/**
 * A reason: its kind and the one extra field that kind carries. {@code ruleIndex} is set for {@link
 * ReasonKind#RULE_MATCH}, {@code flagKey} for {@link ReasonKind#PREREQUISITE_FAILED} and {@code
 * errorCode} for {@link ReasonKind#ERROR}; the others are empty or null.
 *
 * @param kind the kind
 * @param ruleIndex the index in {@code rules} of the matching rule
 * @param flagKey the direct prerequisite that failed, or null
 * @param errorCode the error code, or null
 */
public record Reason(ReasonKind kind, OptionalInt ruleIndex, String flagKey, ErrorCode errorCode) {

  private static final Reason OFF = new Reason(ReasonKind.OFF, OptionalInt.empty(), null, null);
  private static final Reason TARGET_MATCH =
      new Reason(ReasonKind.TARGET_MATCH, OptionalInt.empty(), null, null);
  private static final Reason FALLTHROUGH =
      new Reason(ReasonKind.FALLTHROUGH, OptionalInt.empty(), null, null);

  /** The flag is off. */
  public static Reason off() {
    return OFF;
  }

  /** A target listed the context. */
  public static Reason targetMatch() {
    return TARGET_MATCH;
  }

  /** The rule at {@code index} matched. */
  public static Reason ruleMatch(int index) {
    return new Reason(ReasonKind.RULE_MATCH, OptionalInt.of(index), null, null);
  }

  /** No target or rule decided. */
  public static Reason fallthrough() {
    return FALLTHROUGH;
  }

  /** The direct prerequisite {@code flagKey} was not met. */
  public static Reason prerequisiteFailed(String flagKey) {
    return new Reason(ReasonKind.PREREQUISITE_FAILED, OptionalInt.empty(), flagKey, null);
  }

  /** The evaluation failed with {@code code}. */
  public static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, OptionalInt.empty(), null, code);
  }
}
