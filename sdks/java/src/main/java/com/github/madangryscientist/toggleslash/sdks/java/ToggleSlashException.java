package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * What the client hands to {@code onError} for a failure of its own: a ruleset or stream attempt, a
 * usage request, or counts lost at {@code close}. The message and {@link #code()} never hold a
 * context, a context key, an attribute or the SDK key.
 */
public final class ToggleSlashException extends RuntimeException {

  private static final long serialVersionUID = 1L;

  private final String code;

  ToggleSlashException(String message, String code, Throwable cause) {
    super(message, cause);
    this.code = code;
  }

  /**
   * The {@code lastError} code of the failure ({@code http_<status>}, {@code timeout}, {@code
   * bad_ruleset} or {@code network}), or {@code unsent} for counts lost at close.
   */
  public String code() {
    return code;
  }
}
