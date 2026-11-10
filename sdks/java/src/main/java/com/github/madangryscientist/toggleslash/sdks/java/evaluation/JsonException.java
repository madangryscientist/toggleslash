package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** Text that is not JSON (or is nested too deeply to read). Only {@link Json} throws it. */
public final class JsonException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  JsonException(String message) {
    super(message, null, false, false);
  }
}
