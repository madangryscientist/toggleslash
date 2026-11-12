package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import java.util.Optional;
import java.util.OptionalInt;
import java.util.OptionalLong;

/**
 * What {@code afterEvaluation} receives after every evaluation, a fallback included (contract
 * section 10).
 *
 * @param flagKey the key the caller asked for
 * @param context the context the caller passed
 * @param value what the caller is about to get; for the integer accessor the exact {@link Long}
 * @param variationIndex the served variation; empty for a fallback
 * @param reason why
 * @param etag the ETag of the ruleset the evaluation used; empty before the first load
 * @param flagVersion the flag's version in that ruleset; empty when it has no such flag or the flag
 *     is malformed
 */
public record EvaluationEvent(
    String flagKey,
    Object context,
    Object value,
    OptionalInt variationIndex,
    Reason reason,
    Optional<String> etag,
    OptionalLong flagVersion) {}
