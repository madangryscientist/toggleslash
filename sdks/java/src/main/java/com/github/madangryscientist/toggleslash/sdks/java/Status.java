package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Instant;

/**
 * What the SDK has loaded and how it is doing. It is one consistent snapshot, local only: the
 * server never sees it.
 *
 * @param etag the ETag of the ruleset held, exactly as the server sent it; null before a load
 * @param lastSyncAt when the ruleset was last confirmed current, in whole milliseconds; null if
 *     never
 * @param stream the stream state
 * @param lastError why the latest ruleset or stream attempt failed: {@code http_<status>}, {@code
 *     timeout}, {@code bad_ruleset} or {@code network}; null if it did not
 */
public record Status(String etag, Instant lastSyncAt, StreamState stream, String lastError) {}
