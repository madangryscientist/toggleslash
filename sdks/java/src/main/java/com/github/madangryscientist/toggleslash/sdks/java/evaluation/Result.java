package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.Reason;

/**
 * The result of evaluating a flag.
 *
 * @param value the served variation's JSON value; null when nothing is served
 * @param variationIndex the served variation, or null when nothing is served
 * @param reason why
 */
public record Result(Object value, Integer variationIndex, Reason reason) {}
