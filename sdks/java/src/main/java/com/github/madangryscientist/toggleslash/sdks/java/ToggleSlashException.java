package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.Objects;

/**
 * A failure the SDK absorbed and reports to {@code onError}; it is never thrown at a caller. {@link
 * #code()} is the {@code lastError} code when the failure was a ruleset or stream attempt ({@code
 * http_<status>}, {@code timeout}, {@code bad_ruleset}, {@code network}), and {@code usage} for a
 * usage flush or {@code close}. The message is fixed text: it never holds a URL, a key or a
 * context.
 */
public final class ToggleSlashException extends RuntimeException {
  private static final long serialVersionUID = 1L;

  private final String code;

  ToggleSlashException(String code, String message) {
    super(message);
    this.code = Objects.requireNonNull(code);
  }

  /** The short code of this failure. */
  public String code() {
    return code;
  }
}
