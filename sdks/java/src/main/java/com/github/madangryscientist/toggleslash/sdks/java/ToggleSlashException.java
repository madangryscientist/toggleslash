package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * The error value the SDK builds for {@code onError}. Its message names what failed and never holds
 * a context, a key or a URL.
 */
public final class ToggleSlashException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  private final String code;

  ToggleSlashException(String message, String code) {
    super(message);
    this.code = code;
  }

  /**
   * The short code of the failure.
   *
   * @return a {@link LastError} code for a ruleset or stream failure, {@code usage_<detail>} for a
   *     usage failure
   */
  public String code() {
    return code;
  }
}
