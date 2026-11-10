package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import java.util.Objects;
import java.util.Optional;
import java.util.OptionalInt;

/**
 * What one evaluation did, handed to the {@code afterEvaluation} hook. It carries the context the
 * caller passed, so a hook that sends it anywhere is the caller's decision (the SDK sends none of
 * it).
 *
 * @param flagKey the flag asked for
 * @param context the context passed
 * @param value what the caller got: the served value, or its fallback
 * @param variationIndex the variation served; empty when {@code value} is the fallback
 * @param reason why
 * @param etag the ETag of the ruleset held at this evaluation, as the server sent it; empty before
 *     the first load
 * @param flagVersion the flag's {@code version} in that ruleset; empty when the flag is not in it
 *     or is malformed
 */
public record EvaluationEvent(
    String flagKey,
    Context context,
    Object value,
    OptionalInt variationIndex,
    Reason reason,
    Optional<String> etag,
    Optional<Double> flagVersion) {

  /** Checks that the optionals and the reason are not null. */
  public EvaluationEvent {
    Objects.requireNonNull(variationIndex);
    Objects.requireNonNull(reason);
    Objects.requireNonNull(etag);
    Objects.requireNonNull(flagVersion);
  }
}
