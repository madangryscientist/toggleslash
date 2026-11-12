package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * What {@code afterEvaluation} receives after every evaluation, a fallback included (contract
 * section 10).
 *
 * @param flagKey the key the caller asked for
 * @param context the context the caller passed, as passed
 * @param value what the caller is about to get (an integer accessor's value is a {@link Long})
 * @param variationIndex the index served, or null for a fallback
 * @param reason why
 * @param etag the ETag of the ruleset this evaluation used, or null before the first load
 * @param flagVersion the flag's version in that ruleset, or null when it has no such flag, the flag
 *     is malformed or the version does not fit a signed 64-bit integer
 */
public record EvaluationEvent(
    String flagKey,
    Object context,
    Object value,
    Integer variationIndex,
    Reason reason,
    String etag,
    Long flagVersion) {}
