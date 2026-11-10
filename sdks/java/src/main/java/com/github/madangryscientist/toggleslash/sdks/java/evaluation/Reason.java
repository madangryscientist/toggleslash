package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.Objects;
import java.util.Optional;
import java.util.OptionalInt;

/**
 * Why a flag answered as it did. {@code ruleIndex} is present for {@code RULE_MATCH}, {@code
 * flagKey} for {@code PREREQUISITE_FAILED} (the prerequisite that was not met) and {@code code} for
 * {@code ERROR}.
 */
public record Reason(
    ReasonKind kind, OptionalInt ruleIndex, Optional<String> flagKey, Optional<ErrorCode> code) {

  /** Checks that nothing is null. */
  public Reason {
    Objects.requireNonNull(kind);
    Objects.requireNonNull(ruleIndex);
    Objects.requireNonNull(flagKey);
    Objects.requireNonNull(code);
  }

  /** A reason that carries nothing but its kind. */
  public static Reason of(ReasonKind kind) {
    return new Reason(kind, OptionalInt.empty(), Optional.empty(), Optional.empty());
  }

  /** {@code RULE_MATCH} for the rule at {@code ruleIndex}. */
  public static Reason ruleMatch(int ruleIndex) {
    return new Reason(
        ReasonKind.RULE_MATCH, OptionalInt.of(ruleIndex), Optional.empty(), Optional.empty());
  }

  /** {@code PREREQUISITE_FAILED} for the prerequisite {@code flagKey}. */
  public static Reason prerequisiteFailed(String flagKey) {
    return new Reason(
        ReasonKind.PREREQUISITE_FAILED,
        OptionalInt.empty(),
        Optional.of(flagKey),
        Optional.empty());
  }

  /** {@code ERROR} with {@code code}. */
  public static Reason error(ErrorCode code) {
    return new Reason(ReasonKind.ERROR, OptionalInt.empty(), Optional.empty(), Optional.of(code));
  }
}
