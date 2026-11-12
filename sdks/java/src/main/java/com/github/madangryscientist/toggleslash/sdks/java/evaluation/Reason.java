package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The reason of a result.
 *
 * @param kind the kind
 * @param errorCode the code of an {@code ERROR}, otherwise null
 * @param ruleIndex the index in {@code rules} of a {@code RULE_MATCH}, otherwise null
 * @param flagKey the failed direct prerequisite of a {@code PREREQUISITE_FAILED}, otherwise null
 */
public record Reason(ReasonKind kind, ErrorCode errorCode, Integer ruleIndex, String flagKey) {

  /**
   * A reason that carries nothing beyond its kind.
   *
   * @param kind the kind
   * @return the reason
   */
  public static Reason of(ReasonKind kind) {
    return new Reason(kind, null, null, null);
  }

  /**
   * A {@code RULE_MATCH}.
   *
   * @param index the index in {@code rules}
   * @return the reason
   */
  public static Reason rule(int index) {
    return new Reason(ReasonKind.RULE_MATCH, null, index, null);
  }

  /**
   * A {@code PREREQUISITE_FAILED}.
   *
   * @param flagKey the direct prerequisite that was not met
   * @return the reason
   */
  public static Reason prerequisiteFailed(String flagKey) {
    return new Reason(ReasonKind.PREREQUISITE_FAILED, null, null, flagKey);
  }

  /**
   * An {@code ERROR}.
   *
   * @param code the code
   * @return the reason
   */
  public static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, code, null, null);
  }
}
