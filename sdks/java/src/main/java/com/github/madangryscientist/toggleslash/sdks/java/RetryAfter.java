package com.github.madangryscientist.toggleslash.sdks.java;

import java.net.http.HttpHeaders;

/** Reads {@code Retry-After} of a 429 to section 7.4 of the contract. */
final class RetryAfter {
  /** However long a server asks, a client is back within this. */
  static final long MAX_MILLIS = 300_000L;

  private RetryAfter() {}

  /**
   * How long a 429 asks to wait, capped; -1 for any other status, and for a header that is missing
   * or is not whole decimal seconds.
   */
  static long millis(int status, HttpHeaders headers) {
    if (status != 429) {
      return -1;
    }
    String value = headers.firstValue("Retry-After").orElse("").strip();
    return parse(value);
  }

  static long parse(String value) {
    if (value.isEmpty()) {
      return -1;
    }
    for (int i = 0; i < value.length(); i++) {
      char c = value.charAt(i);
      if (c < '0' || c > '9') {
        return -1;
      }
    }
    if (value.length() > 9) {
      return MAX_MILLIS;
    }
    return Math.min(Long.parseLong(value) * 1000L, MAX_MILLIS);
  }
}
