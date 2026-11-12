package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Instant;
import java.util.Optional;

/**
 * A snapshot of what the client knows about the server (contract section 8). It holds no URL, key,
 * message, context, flag key or count.
 *
 * @param etag the ETag of the ruleset held, as received; empty before the first load
 * @param lastSyncAt when the client last confirmed its ruleset matches the server; empty if never
 * @param stream where the stream stands
 * @param lastError why the latest ruleset or stream attempt failed: {@code http_<status>}, {@code
 *     timeout}, {@code bad_ruleset} or {@code network}; empty when none
 */
public record Status(
    Optional<String> etag,
    Optional<Instant> lastSyncAt,
    StreamState stream,
    Optional<String> lastError) {}
