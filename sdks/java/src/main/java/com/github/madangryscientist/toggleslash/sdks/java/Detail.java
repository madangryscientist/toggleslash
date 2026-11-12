package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * An answer with the variation index and the reason.
 *
 * @param <T> the type of the value
 * @param value the served value, or the caller's fallback
 * @param variationIndex the variation served, or null when the value is the fallback or the flag
 *     serves nothing
 * @param reason why
 */
public record Detail<T>(T value, Integer variationIndex, Reason reason) {}
