package com.github.madangryscientist.toggleslash.sdks.java.json;

/** Thrown when text is not JSON. It carries a position and never any of the text. */
public final class JsonException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  /**
   * Creates the exception.
   *
   * @param message what is wrong
   */
  public JsonException(String message) {
    super(message);
  }
}
