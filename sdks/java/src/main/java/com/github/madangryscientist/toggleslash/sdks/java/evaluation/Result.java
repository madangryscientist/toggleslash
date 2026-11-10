package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.Objects;
import java.util.OptionalInt;

/**
 * The answer to one evaluation. {@code value} is the served variation's JSON value (a {@code Map},
 * {@code List}, {@code String}, {@code Double}, {@code Boolean} or null) and is null when nothing
 * is served. It is shared with the ruleset, so a caller must not change it.
 */
public record Result(Object value, OptionalInt variationIndex, Reason reason) {

  /** Checks that the index and the reason are not null. */
  public Result {
    Objects.requireNonNull(variationIndex);
    Objects.requireNonNull(reason);
  }

  /** An {@code ERROR} result: no value, no variation. */
  public static Result failure(ErrorCode code) {
    return new Result(null, OptionalInt.empty(), Reason.error(code));
  }
}
