package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * What {@code afterEvaluation} receives after every {@code variation} and {@code variationDetail}.
 *
 * @param flagKey the key the caller asked for
 * @param context the context the caller passed
 * @param value what the caller is about to get; a {@link Long} for the integer accessor
 * @param variationIndex the served variation's index, or null for a fallback
 * @param reason the reason
 * @param etag the ETag of the ruleset held at this evaluation, or null before the first load
 * @param flagVersion the flag's version in that ruleset, or null when it has no such flag or the
 *     flag is malformed or the version is above 2^63 minus 1
 */
public record EvaluationEvent(
    String flagKey,
    Object context,
    Object value,
    Integer variationIndex,
    Reason reason,
    String etag,
    Long flagVersion) {}
