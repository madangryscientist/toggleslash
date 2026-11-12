package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * A ruleset, stream or usage request that failed. The code is one of the {@code lastError} codes of
 * section 8 of the contract; the message names the route and the status and nothing else (no URL,
 * key or body).
 */
final class SyncException extends Exception {
  private static final long serialVersionUID = 1L;

  private final String code;

  SyncException(String code, String message, Throwable cause) {
    super(message, cause);
    this.code = code;
  }

  SyncException(String code, String message) {
    this(code, message, null);
  }

  String code() {
    return code;
  }
}
