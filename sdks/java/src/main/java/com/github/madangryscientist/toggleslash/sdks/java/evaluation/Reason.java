package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The reason of an evaluation. {@code errorCode} is set exactly when {@code kind} is {@code ERROR};
 * {@code ruleIndex} only for {@code RULE_MATCH}; {@code flagKey} only for {@code
 * PREREQUISITE_FAILED}.
 *
 * @param kind the kind
 * @param errorCode the code of an {@code ERROR}, otherwise null
 * @param ruleIndex the index in the flag's rules of a {@code RULE_MATCH}, otherwise null
 * @param flagKey the direct prerequisite that failed, otherwise null
 */
public record Reason(ReasonKind kind, ErrorCode errorCode, Integer ruleIndex, String flagKey) {
  /** The flag is off. */
  public static final Reason OFF = new Reason(ReasonKind.OFF, null, null, null);

  /** A target matched. */
  public static final Reason TARGET_MATCH = new Reason(ReasonKind.TARGET_MATCH, null, null, null);

  /** The fallthrough served. */
  public static final Reason FALLTHROUGH = new Reason(ReasonKind.FALLTHROUGH, null, null, null);

  /** An {@code ERROR} with a code. */
  public static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, code, null, null);
  }

  /** A {@code RULE_MATCH} at an index. */
  public static Reason rule(int index) {
    return new Reason(ReasonKind.RULE_MATCH, null, index, null);
  }

  /** A {@code PREREQUISITE_FAILED} naming the prerequisite. */
  public static Reason prerequisiteFailed(String flagKey) {
    return new Reason(ReasonKind.PREREQUISITE_FAILED, null, null, flagKey);
  }
}
