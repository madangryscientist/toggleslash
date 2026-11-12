package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * What {@code onError} receives for a failure the SDK absorbed. It never carries a context, a key
 * or a flag key. The SDK never throws it at a caller.
 */
public final class ToggleSlashException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  private final String code;

  ToggleSlashException(String code, String message, Throwable cause) {
    super(message, cause);
    this.code = code;
  }

  /**
   * The code that {@link Status#lastError()} uses for a ruleset or stream failure ({@code
   * http_500}, {@code timeout}, {@code bad_ruleset}, {@code network}), or a short name for another
   * failure.
   *
   * @return the code
   */
  public String code() {
    return code;
  }
}
