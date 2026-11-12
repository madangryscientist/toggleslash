package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * The codes of {@link Status#lastError()}. A status other than the expected one is {@code
 * http_<n>}, built by {@link #http(int)}.
 */
public final class LastError {
  /** A request ran past {@code requestTimeout}, or the stream went 60 s without a byte. */
  public static final String TIMEOUT = "timeout";

  /** A {@code 200} whose body is not JSON, or is not a ruleset. */
  public static final String BAD_RULESET = "bad_ruleset";

  /** The connection failed, or any other failure. */
  public static final String NETWORK = "network";

  private LastError() {}

  /**
   * The code for an unexpected status.
   *
   * @param status the HTTP status
   * @return {@code http_} and the status
   */
  public static String http(int status) {
    return "http_" + status;
  }
}
