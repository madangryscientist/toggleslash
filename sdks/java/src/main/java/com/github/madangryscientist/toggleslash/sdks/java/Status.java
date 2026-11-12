package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Instant;

/**
 * What the SDK has loaded and how its stream is doing: one consistent snapshot. It holds no URL,
 * header, key, error message, context, flag key or count.
 *
 * @param etag the ETag of the ruleset held, as received; null before the first load
 * @param lastSyncAt when the SDK last confirmed its ruleset matches the server; null if never
 * @param stream the stream state
 * @param lastError why the most recent ruleset or stream attempt failed: {@code http_<status>},
 *     {@code timeout}, {@code bad_ruleset} or {@code network}; null when it did not
 */
public record Status(String etag, Instant lastSyncAt, StreamState stream, String lastError) {}
