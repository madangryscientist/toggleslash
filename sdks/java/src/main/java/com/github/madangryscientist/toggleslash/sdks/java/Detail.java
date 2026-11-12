package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * An answer with the variation and the reason.
 *
 * @param <T> the accessor's type
 * @param value the served value, or the fallback when nothing usable was served
 * @param variationIndex the variation served; null whenever {@code value} is the fallback
 * @param reason why
 */
public record Detail<T>(T value, Integer variationIndex, Reason reason) {}
