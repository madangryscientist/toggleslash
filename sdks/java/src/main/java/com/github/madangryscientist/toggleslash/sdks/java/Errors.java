package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * Builds the errors the SDK hands to {@code onError}; none of them names a context, a key or a
 * flag.
 */
final class Errors {
  private Errors() {}

  /** The error of a request that got no answer. */
  static ToggleSlashException of(Http.Kind kind, String request) {
    if (kind == Http.Kind.TIMEOUT) {
      return new ToggleSlashException(Status.TIMEOUT, request + " timed out");
    }
    return new ToggleSlashException(Status.NETWORK, request + " failed: no answer from the server");
  }

  /** The error of an answer that is not the one wanted. */
  static ToggleSlashException status(int status, String request) {
    return new ToggleSlashException("http_" + status, request + " answered " + status);
  }
}
