package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * What the {@code afterEvaluation} hook receives after each {@code variation} and {@code
 * variationDetail}.
 *
 * @param flagKey the key the caller asked for
 * @param context the context the caller passed; the SDK itself sends none of it anywhere
 * @param value what the caller is about to get (for {@code intVariation} the exact {@code Long})
 * @param variationIndex the served variation's index, or {@code null} for a fallback
 * @param reason why the flag answered as it did
 * @param etag the ETag of the ruleset held at this evaluation, or {@code null} before the first
 *     load
 * @param flagVersion the flag's {@code version} in that ruleset, or {@code null} when the ruleset
 *     has no such flag, the flag is malformed, or the version is above 2^63 minus 1
 */
public record EvaluationEvent(
    String flagKey,
    Object context,
    Object value,
    Integer variationIndex,
    Reason reason,
    String etag,
    Long flagVersion) {}
