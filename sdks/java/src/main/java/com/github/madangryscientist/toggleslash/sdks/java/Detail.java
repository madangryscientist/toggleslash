package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * An evaluation with its variation index and reason.
 *
 * @param <T> the accessor's value type
 * @param value the served value, or the caller's fallback when nothing usable was served
 * @param variationIndex the variation served, or null when {@code value} is the fallback or the
 *     flag serves nothing
 * @param reason why
 */
public record Detail<T>(T value, Integer variationIndex, Reason reason) {}
