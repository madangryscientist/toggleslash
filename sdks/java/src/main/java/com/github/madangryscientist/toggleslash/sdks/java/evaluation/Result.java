package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The answer to one evaluation.
 *
 * @param value the served variation's JSON value ({@code null} when nothing is served); a parsed
 *     tree is unmodifiable
 * @param variationIndex the served variation's index, or {@code null} when none is served
 * @param reason why
 */
public record Result(Object value, Integer variationIndex, Reason reason) {
  /**
   * A failed evaluation: no value, no index, an error reason.
   *
   * @param code the error code
   * @return the result
   */
  public static Result failure(ErrorCode code) {
    return new Result(null, null, Reason.error(code));
  }
}
