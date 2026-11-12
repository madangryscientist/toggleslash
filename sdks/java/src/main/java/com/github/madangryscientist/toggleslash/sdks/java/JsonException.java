package com.github.madangryscientist.toggleslash.sdks.java;

/** Thrown by {@link Json#parse(String)} for text that is not JSON. */
public final class JsonException extends Exception {
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
