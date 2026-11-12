package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * The answer of a {@code variationDetail} call.
 *
 * @param value the value, or the fallback
 * @param variationIndex the served variation's index, or null for a fallback
 * @param reason why
 * @param <T> the value's type
 */
public record Detail<T>(T value, Integer variationIndex, Reason reason) {}
