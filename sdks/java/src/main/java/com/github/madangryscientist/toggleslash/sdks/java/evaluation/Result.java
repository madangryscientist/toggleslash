package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * The outcome of one evaluation.
 *
 * @param value the served variation's JSON value, or null when nothing is served
 * @param variationIndex the index served, or null
 * @param reason why
 */
public record Result(Object value, Integer variationIndex, Reason reason) {
  static Result failure(ErrorCode code) {
    return new Result(null, null, Reason.error(code));
  }
}
