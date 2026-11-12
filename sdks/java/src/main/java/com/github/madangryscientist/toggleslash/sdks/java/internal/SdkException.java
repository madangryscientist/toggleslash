package com.github.madangryscientist.toggleslash.sdks.java.internal;

/** What the SDK hands to {@code onError} for a failure it absorbed; the message holds no secret. */
public final class SdkException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  /**
   * Creates the exception.
   *
   * @param message what failed
   */
  public SdkException(String message) {
    super(message);
  }

  /**
   * Creates the exception with a cause.
   *
   * @param message what failed
   * @param cause why
   */
  public SdkException(String message, Throwable cause) {
    super(message, cause);
  }
}
