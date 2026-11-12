package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Instant;

/**
 * A consistent snapshot of what the SDK has loaded (contract section 8). It holds no URL, key,
 * context or error message.
 *
 * @param etag the ETag of the ruleset held, as received, or null before the first load
 * @param lastSyncAt when the SDK last confirmed its ruleset matches the server, or null if never
 * @param stream how the stream is doing
 * @param lastError why the latest ruleset or stream attempt failed ({@code http_<status>}, {@code
 *     timeout}, {@code bad_ruleset} or {@code network}), or null
 */
public record Status(String etag, Instant lastSyncAt, StreamState stream, String lastError) {}
