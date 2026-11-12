package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * An error the SDK builds and hands to {@code onError}. It never carries a context, a key or a flag
 * key.
 */
public final class ToggleSlashException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  private final String code;

  /**
   * Makes an error.
   *
   * @param code a short code: {@code http_<status>}, {@code timeout}, {@code bad_ruleset}, {@code
   *     network} or {@code usage_lost}
   * @param message what happened, without any context data
   */
  public ToggleSlashException(String code, String message) {
    super(message);
    this.code = code;
  }

  /**
   * The short code of this error.
   *
   * @return the code
   */
  public String code() {
    return code;
  }
}
