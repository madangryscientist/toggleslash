package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * The result of a {@code ...VariationDetail} call.
 *
 * @param <T> the type of the value
 * @param value the served value, or the caller's fallback
 * @param variationIndex the index into the flag's variations, or null whenever the value is the
 *     fallback
 * @param reason why
 */
public record Detail<T>(T value, Integer variationIndex, Reason reason) {}
