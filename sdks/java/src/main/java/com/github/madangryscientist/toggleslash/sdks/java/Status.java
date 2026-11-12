package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * A snapshot of the client's state.
 *
 * @param etag the ETag of the held ruleset, or null
 * @param lastSyncAt epoch milliseconds of the last successful sync, or null
 * @param stream the stream state
 * @param lastError one of {@code http_<status>}, {@code timeout}, {@code bad_ruleset}, {@code
 *     network}, or null
 */
public record Status(String etag, Long lastSyncAt, StreamState stream, String lastError) {}
