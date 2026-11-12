package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * What the evaluator returns: the served value, its index (null when none was served) and the
 * reason.
 *
 * @param value the served value, a JSON value as {@link Json} reads it, or null
 * @param variationIndex the index into the flag's variations, or null
 * @param reason why
 */
record EvalResult(Object value, Integer variationIndex, Reason reason) {
  static EvalResult failure(ErrorCode code) {
    return new EvalResult(null, null, Reason.error(code));
  }
}
