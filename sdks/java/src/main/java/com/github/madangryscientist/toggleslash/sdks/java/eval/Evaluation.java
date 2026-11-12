package com.github.madangryscientist.toggleslash.sdks.java.eval;

import com.github.madangryscientist.toggleslash.sdks.java.Reason;

/**
 * What evaluating a flag answered.
 *
 * @param value the served variation's JSON value, or null when nothing is served
 * @param variationIndex the served index, or null
 * @param reason the reason
 */
public record Evaluation(Object value, Integer variationIndex, Reason reason) {}
