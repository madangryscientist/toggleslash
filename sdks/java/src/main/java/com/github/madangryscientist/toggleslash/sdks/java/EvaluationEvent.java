package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;

/**
 * What the {@link AfterEvaluation} hook is told about one call.
 *
 * @param flagKey the key the caller asked for
 * @param context the context the caller passed
 * @param value what the caller gets, in the accessor's own boxed type
 * @param variationIndex the served variation's index, or null for a fallback
 * @param reason why
 * @param etag the ETag of the ruleset held at the evaluation, or null before the first load
 * @param flagVersion the flag's version in that ruleset, or null when it has none or is malformed
 */
public record EvaluationEvent(
    String flagKey,
    Object context,
    Object value,
    Integer variationIndex,
    Reason reason,
    String etag,
    Long flagVersion) {}
