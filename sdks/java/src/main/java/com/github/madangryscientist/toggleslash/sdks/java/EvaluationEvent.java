package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * What {@link Hook} receives after an evaluation.
 *
 * @param flagKey the flag asked for
 * @param context the context as the caller passed it
 * @param value the value the caller got
 * @param variationIndex the served index, or null for a fallback
 * @param reason the reason
 * @param etag the ETag of the ruleset the answer came from, or null
 * @param flagVersion the version of the flag that answered, or null
 */
public record EvaluationEvent(
    String flagKey,
    Object context,
    Object value,
    Integer variationIndex,
    Reason reason,
    String etag,
    Long flagVersion) {}
