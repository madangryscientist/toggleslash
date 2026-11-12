package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * The answer of a {@code ...Detail} call: the value the caller gets and why.
 *
 * @param <T> the type of the value
 * @param value the served value, or the fallback
 * @param variationIndex the index of the served variation, or {@code null} when the answer is the
 *     fallback
 * @param reason why the flag answered as it did
 */
public record Detail<T>(T value, Integer variationIndex, Reason reason) {}
