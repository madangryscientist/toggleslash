package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.concurrent.atomic.AtomicLong;

/**
 * A 429's request to wait: a point in time, not a sleep. A later hold can lengthen it and never
 * shorten it.
 */
final class Hold {
  private final AtomicLong until = new AtomicLong();

  /** Holds until {@code atMillis} (on the clock's scale) unless it is already held past that. */
  void extend(long atMillis) {
    until.accumulateAndGet(atMillis, Math::max);
  }

  /** The milliseconds left of the hold at {@code now}; 0 when it has passed. */
  long remaining(long now) {
    return Math.max(0, until.get() - now);
  }

  /**
   * How long a 429 asks to wait. Only whole decimal digits count (surrounding spaces are ignored);
   * anything else, a date, a fraction or a negative number, is no hold.
   *
   * @param header the {@code Retry-After} value, or null
   * @param capMillis the longest hold
   * @return the wait in milliseconds, capped, or -1 for no valid hold
   */
  static long parse(String header, long capMillis) {
    if (header == null) {
      return -1;
    }
    String text = header.strip();
    if (text.isEmpty()) {
      return -1;
    }
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      if (c < '0' || c > '9') {
        return -1;
      }
    }
    // More than twelve digits is already past any cap, and keeps the arithmetic in range.
    if (text.length() > 12) {
      return capMillis;
    }
    return Math.min(Long.parseLong(text) * 1000, capMillis);
  }
}
