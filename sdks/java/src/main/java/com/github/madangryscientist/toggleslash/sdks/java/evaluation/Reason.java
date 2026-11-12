package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Why a value was served.
 *
 * @param kind the kind
 * @param ruleIndex the index of the matching rule, for {@link ReasonKind#RULE_MATCH} only
 * @param flagKey the prerequisite that was not met, for {@link ReasonKind#PREREQUISITE_FAILED} only
 * @param errorCode the error, for {@link ReasonKind#ERROR} only
 */
public record Reason(ReasonKind kind, Integer ruleIndex, String flagKey, ErrorCode errorCode) {
  /** The reason for a flag that is off. */
  public static final Reason OFF = new Reason(ReasonKind.OFF, null, null, null);

  /** The reason for a target match. */
  public static final Reason TARGET_MATCH = new Reason(ReasonKind.TARGET_MATCH, null, null, null);

  /** The reason for the fallthrough. */
  public static final Reason FALLTHROUGH = new Reason(ReasonKind.FALLTHROUGH, null, null, null);

  /**
   * A rule match.
   *
   * @param index the rule's index
   * @return the reason
   */
  public static Reason ruleMatch(int index) {
    return new Reason(ReasonKind.RULE_MATCH, index, null, null);
  }

  /**
   * An unmet prerequisite.
   *
   * @param flagKey the prerequisite's flag key
   * @return the reason
   */
  public static Reason prerequisiteFailed(String flagKey) {
    return new Reason(ReasonKind.PREREQUISITE_FAILED, null, flagKey, null);
  }

  /**
   * An error.
   *
   * @param code the error code
   * @return the reason
   */
  public static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, null, null, code);
  }

  /**
   * The reason as the protocols spell it: {@code kind}, then {@code ruleIndex}, {@code flagKey} or
   * {@code code} when the kind has one.
   *
   * @return a new map
   */
  public Map<String, Object> toMap() {
    Map<String, Object> map = new LinkedHashMap<>();
    map.put("kind", kind.name());
    if (ruleIndex != null) {
      map.put("ruleIndex", ruleIndex.doubleValue());
    }
    if (flagKey != null) {
      map.put("flagKey", flagKey);
    }
    if (errorCode != null) {
      map.put("code", errorCode.name());
    }
    return map;
  }
}
