package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.concurrent.atomic.AtomicLong;

/**
 * A Retry-After hold: an absolute monotonic time before which nothing is sent. It only ever moves
 * later.
 */
final class Hold {
  /** The longest a Retry-After is honoured. */
  static final long CAP_SECONDS = 300;

  private static final long NONE = Long.MIN_VALUE;

  private final Pacer pacer;
  private final AtomicLong until = new AtomicLong(NONE);

  Hold(Pacer pacer) {
    this.pacer = pacer;
  }

  /**
   * Reads a Retry-After value. It counts only as whole decimal seconds; surrounding spaces are
   * ignored.
   *
   * @param header the header value, or null
   * @return the seconds, capped at 300, or -1 when the value does not count
   */
  static long parseSeconds(String header) {
    if (header == null) {
      return -1;
    }
    int start = 0;
    int end = header.length();
    while (start < end && isSpace(header.charAt(start))) {
      start++;
    }
    while (end > start && isSpace(header.charAt(end - 1))) {
      end--;
    }
    if (start == end) {
      return -1;
    }
    long seconds = 0;
    for (int i = start; i < end; i++) {
      char c = header.charAt(i);
      if (c < '0' || c > '9') {
        return -1;
      }
      if (seconds <= CAP_SECONDS) {
        seconds = seconds * 10 + (c - '0');
      }
    }
    return Math.min(seconds, CAP_SECONDS);
  }

  private static boolean isSpace(char c) {
    return c == ' ' || c == '\t';
  }

  /**
   * Holds for a number of seconds from now; a shorter hold than the one in force changes nothing.
   *
   * @param seconds seconds already capped
   */
  void extend(long seconds) {
    long target = pacer.nanoTime() + seconds * 1_000_000_000L;
    until.accumulateAndGet(
        target, (current, next) -> current == NONE || next - current > 0 ? next : current);
  }

  /**
   * How long the hold still lasts.
   *
   * @return nanoseconds, 0 when there is none
   */
  long remainingNanos() {
    long current = until.get();
    if (current == NONE) {
      return 0;
    }
    long left = current - pacer.nanoTime();
    return Math.max(left, 0);
  }
}
