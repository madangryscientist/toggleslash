package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * An evaluation with its variation index and reason.
 *
 * @param value the served value, or the caller's fallback
 * @param variationIndex the index of the served variation, or null when the value is the fallback
 * @param reason why
 * @param <T> the value type
 */
public record Detail<T>(T value, Integer variationIndex, Reason reason) {}
