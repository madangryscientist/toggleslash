package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * One snapshot of what the SDK has loaded and how its stream is doing. It is local state: the
 * server never sees it, and it holds no URL, key, error message, context, flag key or count.
 *
 * @param etag the ETag of the ruleset held, as received, or {@code null} before the first load
 * @param lastSyncAt when the ruleset was last confirmed current, as epoch milliseconds, or {@code
 *     null} if never
 * @param stream the stream state
 * @param lastError why the most recent ruleset or stream attempt failed ({@code http_<status>},
 *     {@link #TIMEOUT}, {@link #BAD_RULESET} or {@link #NETWORK}), or {@code null}
 */
public record Status(String etag, Long lastSyncAt, StreamState stream, String lastError) {
  /** The {@code lastError} of a request or a stream that went quiet for too long. */
  public static final String TIMEOUT = "timeout";

  /** The {@code lastError} of a {@code 200} whose body is not a ruleset. */
  public static final String BAD_RULESET = "bad_ruleset";

  /** The {@code lastError} of a connection that failed, or any other failure. */
  public static final String NETWORK = "network";
}
