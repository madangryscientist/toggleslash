package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.OptionalLong;

/** Reads a {@code Retry-After} value (contract 7.4). */
final class RetryAfter {

  /** The longest wait the SDK honours, in seconds. */
  static final long CAP_SECONDS = 300;

  private RetryAfter() {}

  /**
   * The wait in seconds, capped at 300, when {@code value} is one or more decimal digits and
   * nothing else (spaces around it are ignored); empty for a date, a fraction, a sign or no header.
   */
  static OptionalLong parse(String value) {
    if (value == null) {
      return OptionalLong.empty();
    }
    int start = 0;
    int end = value.length();
    while (start < end && value.charAt(start) == ' ') {
      start++;
    }
    while (end > start && value.charAt(end - 1) == ' ') {
      end--;
    }
    if (start == end) {
      return OptionalLong.empty();
    }
    long seconds = 0;
    for (int i = start; i < end; i++) {
      char c = value.charAt(i);
      if (c < '0' || c > '9') {
        return OptionalLong.empty();
      }
      seconds = Math.min(CAP_SECONDS, seconds * 10 + (c - '0'));
    }
    return OptionalLong.of(seconds);
  }
}
