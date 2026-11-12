package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * What the SDK passes to {@code onError} for a failure it absorbed. The message names the route and
 * the outcome and never holds a context, a context key, an attribute or the SDK key.
 */
public final class ToggleSlashException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  /**
   * Creates the exception.
   *
   * @param message what failed
   */
  public ToggleSlashException(String message) {
    super(message);
  }

  /**
   * Creates the exception with a cause.
   *
   * @param message what failed
   * @param cause the underlying failure
   */
  public ToggleSlashException(String message, Throwable cause) {
    super(message, cause);
  }
}
