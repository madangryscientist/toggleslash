package com.github.madangryscientist.toggleslash.sdks.java.json;

/** Raised when a text is not JSON, or nests deeper than the parser allows. */
public final class JsonException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  /**
   * Builds the exception.
   *
   * @param message what was wrong, without any of the text parsed
   */
  public JsonException(String message) {
    super(message);
  }
}
