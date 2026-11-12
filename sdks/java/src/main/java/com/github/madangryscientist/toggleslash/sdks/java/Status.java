package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Instant;

/**
 * One snapshot of the client's sync state. It holds no URL, header, key, message, context, flag key
 * or count.
 *
 * @param etag the ETag of the ruleset held, or null before the first load
 * @param lastSyncAt when the ruleset was last confirmed current, or null if never
 * @param stream the stream state
 * @param lastError {@code http_<status>}, {@link #TIMEOUT}, {@link #BAD_RULESET}, {@link #NETWORK},
 *     or null
 */
public record Status(String etag, Instant lastSyncAt, StreamState stream, String lastError) {
  /** The request or stream exceeded its time bound. */
  public static final String TIMEOUT = "timeout";

  /** A 200 answer that is not a ruleset. */
  public static final String BAD_RULESET = "bad_ruleset";

  /** The connection failed, or any other failure. */
  public static final String NETWORK = "network";
}
