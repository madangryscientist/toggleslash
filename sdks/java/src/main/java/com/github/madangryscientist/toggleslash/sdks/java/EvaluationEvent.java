package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * What one evaluation did, handed to the {@code afterEvaluation} hook. It carries the context the
 * caller passed, so a hook that sends it anywhere is the application's decision.
 *
 * @param flagKey the key the caller asked for
 * @param context the context the caller passed
 * @param value what the caller is about to get, in the accessor's own type (a {@code Long} for the
 *     integer accessor, a {@code Double} for a number, a {@code Boolean}, a {@code String}, or the
 *     JSON value types for the JSON accessor)
 * @param variationIndex the variation served, or null for a fallback
 * @param reason why
 * @param etag the ETag of the ruleset held at this evaluation, or null before the first load
 * @param flagVersion the flag's version in that ruleset, or null when the ruleset has no such flag,
 *     the flag is malformed or names a missing or malformed segment, or the version does not fit a
 *     signed 64-bit integer
 */
public record EvaluationEvent(
    String flagKey,
    Object context,
    Object value,
    Integer variationIndex,
    Reason reason,
    String etag,
    Long flagVersion) {}
