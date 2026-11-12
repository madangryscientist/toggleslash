package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * What one evaluation did, handed to the {@code afterEvaluation} hook. It carries the context the
 * caller passed, so a hook that sends it anywhere is the caller's decision: the SDK itself sends
 * none of it.
 *
 * @param flagKey the key the caller asked for
 * @param context the context the caller passed, or null
 * @param value what the caller is about to get; the integer accessor's value is a {@code Long}
 * @param variationIndex the variation served, or null for a fallback
 * @param reason why
 * @param etag the ETag of the ruleset held at this evaluation, as received; null before the first
 *     load
 * @param flagVersion the flag's {@code version} in that ruleset, or null when the ruleset has no
 *     such flag or the flag is malformed (or the version is above 2^63 minus 1)
 */
public record EvaluationEvent(
    String flagKey,
    Context context,
    Object value,
    Integer variationIndex,
    Reason reason,
    String etag,
    Long flagVersion) {}
