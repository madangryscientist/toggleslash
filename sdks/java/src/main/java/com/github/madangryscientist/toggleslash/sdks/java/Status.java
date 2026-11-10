package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Instant;
import java.util.Objects;
import java.util.Optional;

/**
 * What this SDK has loaded and how its stream is doing: local state only, which the server never
 * sees.
 *
 * @param etag the ETag of the ruleset held, as the server sent it; empty before the first load
 * @param lastSyncAt when the SDK last confirmed its ruleset matches the server; empty if never
 * @param stream the stream's state
 * @param lastError why the most recent ruleset or stream attempt failed ({@code http_<status>},
 *     {@code timeout}, {@code bad_ruleset} or {@code network}); empty if it did not
 */
public record Status(
    Optional<String> etag,
    Optional<Instant> lastSyncAt,
    StreamState stream,
    Optional<String> lastError) {

  /** Checks that nothing is null. */
  public Status {
    Objects.requireNonNull(etag);
    Objects.requireNonNull(lastSyncAt);
    Objects.requireNonNull(stream);
    Objects.requireNonNull(lastError);
  }
}
