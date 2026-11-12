package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * What one evaluation did, handed to the {@code afterEvaluation} hook. It carries the context the
 * caller passed, so a hook that sends it anywhere is the caller's decision.
 *
 * @param flagKey the flag asked for
 * @param context the context the caller passed
 * @param value what the caller got: a {@code Boolean}, {@code String}, {@code Double}, {@code Long}
 *     or {@code Json}
 * @param variationIndex the variation served; null when {@code value} is the fallback
 * @param reason why
 * @param etag the ETag of the ruleset used, as the server sent it; null before the first load
 * @param flagVersion the flag's version in that ruleset; null when the flag is missing or malformed
 *     (or above 2^63 minus 1)
 */
public record EvaluationEvent(
    String flagKey,
    Context context,
    Object value,
    Integer variationIndex,
    Reason reason,
    String etag,
    Long flagVersion) {}
