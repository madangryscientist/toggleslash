package com.github.madangryscientist.toggleslash.sdks.java;

/** Reads a {@code Retry-After} header, which counts only as whole decimal seconds. */
final class RetryAfter {
  static final long CAP_SECONDS = 300;

  private RetryAfter() {}

  /** The wait in seconds, capped at 300, or -1 when the header does not count. */
  static long seconds(String header) {
    if (header == null) {
      return -1;
    }
    int start = 0;
    int end = header.length();
    while (start < end && header.charAt(start) == ' ') {
      start++;
    }
    while (end > start && header.charAt(end - 1) == ' ') {
      end--;
    }
    if (start == end) {
      return -1;
    }
    long value = 0;
    for (int i = start; i < end; i++) {
      char c = header.charAt(i);
      if (c < '0' || c > '9') {
        return -1;
      }
      value = Math.min(CAP_SECONDS, value * 10 + (c - '0'));
    }
    return value;
  }
}
