package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * The value, variation index and reason of one evaluation.
 *
 * @param <T> the value type
 * @param value the served value, or the caller's fallback
 * @param variationIndex the served variation's index, or null whenever the value is the fallback
 * @param reason why
 */
public record Detail<T>(T value, Integer variationIndex, Reason reason) {}
