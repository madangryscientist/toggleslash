package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Why a value was served. Only the member that belongs to the kind is set: {@code ruleIndex} for
 * {@code RULE_MATCH}, {@code flagKey} for {@code PREREQUISITE_FAILED} and {@code code} for {@code
 * ERROR}.
 *
 * @param kind the kind of reason
 * @param ruleIndex the index of the rule that matched, or null
 * @param flagKey the key of the prerequisite that failed, or null
 * @param code the error code, or null
 */
public record Reason(ReasonKind kind, Integer ruleIndex, String flagKey, ErrorCode code) {
  /**
   * A reason that carries nothing but its kind.
   *
   * @param kind the kind, which must not be {@code ERROR}, {@code RULE_MATCH} or {@code
   *     PREREQUISITE_FAILED}
   * @return the reason
   */
  public static Reason of(ReasonKind kind) {
    return new Reason(kind, null, null, null);
  }

  /**
   * An {@code ERROR} reason.
   *
   * @param code the error code
   * @return the reason
   */
  public static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, null, null, code);
  }

  /**
   * The reason as the JSON object the test service and the wire use.
   *
   * @return an object with {@code kind} and the member that belongs to it
   */
  public Map<String, Object> toJson() {
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("kind", kind.name());
    if (ruleIndex != null) {
      out.put("ruleIndex", ruleIndex.doubleValue());
    }
    if (flagKey != null) {
      out.put("flagKey", flagKey);
    }
    if (code != null) {
      out.put("code", code.name());
    }
    return out;
  }
}
