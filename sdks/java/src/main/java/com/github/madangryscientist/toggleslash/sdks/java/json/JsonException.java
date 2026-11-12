package com.github.madangryscientist.toggleslash.sdks.java.json;

/** Thrown by {@link Json#parse(String)} for text that is not a JSON document the parser accepts. */
public final class JsonException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  /**
   * Creates the exception.
   *
   * @param message what was wrong, with no part of the document in it
   */
  public JsonException(String message) {
    super(message);
  }
}
