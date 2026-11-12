package com.github.madangryscientist.toggleslash.sdks.java.json;

/** Thrown by {@link Json#parse} when the text is not valid JSON. */
public final class JsonException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  /**
   * Creates the exception.
   *
   * @param message what was wrong, without any of the text that was parsed
   */
  public JsonException(String message) {
    super(message, null, false, false);
  }
}
