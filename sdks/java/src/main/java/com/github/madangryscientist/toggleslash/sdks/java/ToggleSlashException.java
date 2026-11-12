package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * A failure the client absorbed, as {@code onError} receives it. It is never thrown at a caller.
 * Its message holds no context, key or attribute.
 */
public final class ToggleSlashException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  private final String code;

  ToggleSlashException(String code, String message, Throwable cause) {
    super(message, cause);
    this.code = code;
  }

  /**
   * The code the failure sets in {@link Status#lastError()}, or null for a failure that does not.
   *
   * @return {@code http_<status>}, {@code timeout}, {@code bad_ruleset}, {@code network} or null
   */
  public String code() {
    return code;
  }
}
