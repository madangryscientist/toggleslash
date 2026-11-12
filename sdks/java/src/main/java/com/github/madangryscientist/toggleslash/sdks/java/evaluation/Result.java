package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * What an evaluation answers.
 *
 * @param value the served JSON value, or null when nothing is served
 * @param variationIndex the served variation, or null when nothing is served
 * @param reason why
 */
public record Result(Object value, Integer variationIndex, Reason reason) {}
