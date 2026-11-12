package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * Why a flag evaluated as it did.
 *
 * @param kind what happened
 * @param flagKey the failed direct prerequisite, only for {@link ReasonKind#PREREQUISITE_FAILED}
 * @param ruleIndex the index in the flag's rules, only for {@link ReasonKind#RULE_MATCH}
 * @param errorCode the cause, only for {@link ReasonKind#ERROR}
 */
public record Reason(ReasonKind kind, String flagKey, Integer ruleIndex, ErrorCode errorCode) {
  static final Reason OFF = new Reason(ReasonKind.OFF, null, null, null);
  static final Reason TARGET_MATCH = new Reason(ReasonKind.TARGET_MATCH, null, null, null);
  static final Reason FALLTHROUGH = new Reason(ReasonKind.FALLTHROUGH, null, null, null);

  static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, null, null, code);
  }

  static Reason ruleMatch(int index) {
    return new Reason(ReasonKind.RULE_MATCH, null, index, null);
  }

  static Reason prerequisiteFailed(String flagKey) {
    return new Reason(ReasonKind.PREREQUISITE_FAILED, flagKey, null, null);
  }

  /**
   * The reason as the harness and the wire spell it.
   *
   * @return a short readable form such as {@code RULE_MATCH(0)}
   */
  @Override
  public String toString() {
    switch (kind) {
      case ERROR:
        return "ERROR(" + errorCode + ")";
      case RULE_MATCH:
        return "RULE_MATCH(" + ruleIndex + ")";
      case PREREQUISITE_FAILED:
        return "PREREQUISITE_FAILED(" + flagKey + ")";
      default:
        return kind.name();
    }
  }
}
