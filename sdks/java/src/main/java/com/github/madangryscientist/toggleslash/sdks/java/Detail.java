package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import java.util.Objects;
import java.util.OptionalInt;

/**
 * One evaluation, in full.
 *
 * @param value the served value, or the fallback when nothing usable was served
 * @param variationIndex the variation served; empty when {@code value} is the fallback or the flag
 *     serves nothing
 * @param reason why
 * @param <T> the type of the value
 */
public record Detail<T>(T value, OptionalInt variationIndex, Reason reason) {

  /** Checks that the index and the reason are not null. */
  public Detail {
    Objects.requireNonNull(variationIndex);
    Objects.requireNonNull(reason);
  }
}
