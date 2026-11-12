package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

/**
 * A "Retry-After" hold: a monotonic point in time before which nothing is sent. It is a time, not a
 * sleep, and it only ever lengthens.
 */
final class Hold {
  /** However long a server asks, a client is back within this (contract 7.4). */
  static final long LONGEST_NANOS = TimeUnit.SECONDS.toNanos(300);

  private final AtomicLong until = new AtomicLong(System.nanoTime());

  /** Holds for {@code nanos} from now, unless a longer hold is already in force. */
  void extend(long nanos) {
    long target = System.nanoTime() + Math.min(nanos, LONGEST_NANOS);
    until.accumulateAndGet(
        target, (current, candidate) -> candidate - current > 0 ? candidate : current);
  }

  /** The point in time (a {@code System.nanoTime} reading) the hold lasts until. */
  long until() {
    return until.get();
  }

  /** How many nanoseconds of the hold remain, zero if it has passed. */
  long remainingNanos() {
    return Math.max(0, until.get() - System.nanoTime());
  }

  boolean active() {
    return remainingNanos() > 0;
  }

  /**
   * Reads a {@code Retry-After} value: whole decimal seconds only, surrounding spaces ignored.
   *
   * @return the seconds asked for, capped at 300, or -1 when the value does not count
   */
  static long parseSeconds(String header) {
    if (header == null) {
      return -1;
    }
    String trimmed = header.strip();
    if (trimmed.isEmpty()) {
      return -1;
    }
    for (int i = 0; i < trimmed.length(); i++) {
      char c = trimmed.charAt(i);
      if (c < '0' || c > '9') {
        return -1;
      }
    }
    if (trimmed.length() > 9) {
      return 300;
    }
    return Math.min(Long.parseLong(trimmed), 300);
  }
}
