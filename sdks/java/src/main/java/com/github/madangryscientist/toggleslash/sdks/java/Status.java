package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Instant;

/**
 * What the client has loaded and how its stream is doing: one consistent snapshot, local to the
 * process. It holds no URL, header, key, message, context, flag key or count.
 *
 * @param etag the ETag of the ruleset held, as received, or null before the first load
 * @param lastSyncAt when the ruleset was last confirmed current, or null if never
 * @param stream the stream state
 * @param lastError why the most recent ruleset or stream attempt failed: {@code http_<status>},
 *     {@code timeout}, {@code bad_ruleset} or {@code network}; null if it did not
 */
public record Status(String etag, Instant lastSyncAt, StreamState stream, String lastError) {
  /** The {@code lastError} code for a request that exceeded its time. */
  public static final String TIMEOUT = "timeout";

  /** The {@code lastError} code for an answer that is not a ruleset. */
  public static final String BAD_RULESET = "bad_ruleset";

  /** The {@code lastError} code for a failed connection or any other failure. */
  public static final String NETWORK = "network";
}
