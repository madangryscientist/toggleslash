package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import java.util.OptionalInt;

/**
 * The answer of a {@code ...VariationDetail} method.
 *
 * @param value the served value, or the caller's fallback
 * @param variationIndex the served variation; empty for a fallback
 * @param reason why
 * @param <T> the accessor's type
 */
public record Evaluation<T>(T value, OptionalInt variationIndex, Reason reason) {}
