package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * An error the SDK builds for {@link Options.Builder#onError}. Its message never holds a context, a
 * context key, an attribute or the SDK key.
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
}
