package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * The reason of an evaluation.
 *
 * @param kind the kind
 * @param ruleIndex the index of the matching rule for {@code RULE_MATCH}, otherwise null
 * @param flagKey the failed prerequisite's key for {@code PREREQUISITE_FAILED}, otherwise null
 * @param errorCode the code for {@code ERROR}, otherwise null
 */
public record Reason(ReasonKind kind, Integer ruleIndex, String flagKey, ErrorCode errorCode) {
  /**
   * An error reason.
   *
   * @param code the code
   * @return the reason
   */
  public static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, null, null, code);
  }

  /**
   * A reason that carries only its kind.
   *
   * @param kind the kind
   * @return the reason
   */
  public static Reason of(ReasonKind kind) {
    return new Reason(kind, null, null, null);
  }
}
