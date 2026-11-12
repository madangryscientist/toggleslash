package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The reason half of a result.
 *
 * @param kind what happened
 * @param ruleIndex the rule, for {@link ReasonKind#RULE_MATCH}; otherwise null
 * @param flagKey the prerequisite, for {@link ReasonKind#PREREQUISITE_FAILED}; otherwise null
 * @param code the error, for {@link ReasonKind#ERROR}; otherwise null
 */
public record Reason(ReasonKind kind, Integer ruleIndex, String flagKey, ErrorCode code) {
  /** The flag is off. */
  public static final Reason OFF = new Reason(ReasonKind.OFF, null, null, null);

  /** A target matched. */
  public static final Reason TARGET_MATCH = new Reason(ReasonKind.TARGET_MATCH, null, null, null);

  /** The fallthrough served. */
  public static final Reason FALLTHROUGH = new Reason(ReasonKind.FALLTHROUGH, null, null, null);

  /**
   * A rule matched.
   *
   * @param index the rule's place
   * @return the reason
   */
  public static Reason ruleMatch(int index) {
    return new Reason(ReasonKind.RULE_MATCH, index, null, null);
  }

  /**
   * A prerequisite was not met.
   *
   * @param flagKey the prerequisite's flag
   * @return the reason
   */
  public static Reason prerequisiteFailed(String flagKey) {
    return new Reason(ReasonKind.PREREQUISITE_FAILED, null, flagKey, null);
  }

  /**
   * An error.
   *
   * @param code the code
   * @return the reason
   */
  public static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, null, null, code);
  }
}
