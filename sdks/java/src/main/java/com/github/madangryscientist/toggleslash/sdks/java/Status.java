package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Instant;

/**
 * What the SDK has loaded and how its stream is doing, read as one snapshot. It holds no URL,
 * header, key, error message, context, flag key or count.
 *
 * @param etag the ETag of the ruleset held, as received, or null before the first load
 * @param lastSyncAt when the SDK last confirmed its ruleset matches the server, or null if never
 * @param stream the stream state
 * @param lastError why the latest ruleset or stream attempt failed ({@link LastError}), or null
 */
public record Status(String etag, Instant lastSyncAt, StreamState stream, String lastError) {}
