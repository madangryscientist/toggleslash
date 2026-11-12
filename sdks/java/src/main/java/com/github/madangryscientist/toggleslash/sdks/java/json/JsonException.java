package com.github.madangryscientist.toggleslash.sdks.java.json;

/** The text is not JSON, or a value cannot be written as JSON. */
public final class JsonException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  /** Creates the exception with a message. */
  public JsonException(String message) {
    super(message);
  }
}
