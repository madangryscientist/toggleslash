package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.OptionalInt;

/**
 * What an evaluation produced. {@code value} is a JSON value (see {@link Json}), null when there is
 * none or the served variation is {@code null}; {@code variationIndex} tells those apart.
 *
 * @param value the served value
 * @param variationIndex the served variation, empty when none was served
 * @param reason why
 */
public record EvaluationResult(Object value, OptionalInt variationIndex, Reason reason) {

  /** An {@code ERROR} result with {@code code}. */
  public static EvaluationResult failure(ErrorCode code) {
    return new EvaluationResult(null, OptionalInt.empty(), Reason.error(code));
  }
}
