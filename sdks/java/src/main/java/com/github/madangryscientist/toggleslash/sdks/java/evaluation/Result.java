package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * What the evaluator answers: the served value (null when nothing is served), its variation index
 * (null when none) and the reason.
 *
 * @param value the served variation's JSON value, null when nothing is served
 * @param variationIndex the served variation's index, null when none
 * @param reason why
 */
public record Result(Object value, Integer variationIndex, Reason reason) {}
