package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * Why a flag answered as it did.
 *
 * <p>Only the fields that apply to the {@code kind} are set; the others are {@code null}.
 *
 * @param kind the kind of reason
 * @param ruleIndex the index of the matching rule, for {@link ReasonKind#RULE_MATCH}
 * @param flagKey the prerequisite that failed, for {@link ReasonKind#PREREQUISITE_FAILED}
 * @param code the error, for {@link ReasonKind#ERROR}
 */
public record Reason(ReasonKind kind, Integer ruleIndex, String flagKey, ErrorCode code) {
  private static final Reason OFF = new Reason(ReasonKind.OFF, null, null, null);
  private static final Reason TARGET_MATCH = new Reason(ReasonKind.TARGET_MATCH, null, null, null);
  private static final Reason FALLTHROUGH = new Reason(ReasonKind.FALLTHROUGH, null, null, null);

  /**
   * The reason of an off flag.
   *
   * @return the reason
   */
  public static Reason off() {
    return OFF;
  }

  /**
   * The reason of a context listed in a target.
   *
   * @return the reason
   */
  public static Reason targetMatch() {
    return TARGET_MATCH;
  }

  /**
   * The reason of the fallthrough.
   *
   * @return the reason
   */
  public static Reason fallthrough() {
    return FALLTHROUGH;
  }

  /**
   * The reason of a matching rule.
   *
   * @param ruleIndex the index of the rule
   * @return the reason
   */
  public static Reason ruleMatch(int ruleIndex) {
    return new Reason(ReasonKind.RULE_MATCH, ruleIndex, null, null);
  }

  /**
   * The reason of a flag whose prerequisite is not met.
   *
   * @param flagKey the direct prerequisite
   * @return the reason
   */
  public static Reason prerequisiteFailed(String flagKey) {
    return new Reason(ReasonKind.PREREQUISITE_FAILED, null, flagKey, null);
  }

  /**
   * The reason of an error.
   *
   * @param code the error
   * @return the reason
   */
  public static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, null, null, code);
  }
}
