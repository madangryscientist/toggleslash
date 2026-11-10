package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.function.LongSupplier;

/**
 * A server's "wait this long" (a {@code 429} with {@code Retry-After}): a deadline on a monotonic
 * clock that only moves later, never longer than {@link #MAX_SECONDS} from the moment it is asked
 * for.
 */
final class Hold {
  /** However long a server asks, a client is back within this. */
  static final long MAX_SECONDS = 300;

  private final LongSupplier nanoTime;
  private long until;
  private boolean set;

  Hold(LongSupplier nanoTime) {
    this.nanoTime = nanoTime;
  }

  /** Holds for {@code seconds} from now (capped); a shorter ask never shortens a longer hold. */
  synchronized void extend(long seconds) {
    long capped = Math.min(Math.max(seconds, 0), MAX_SECONDS);
    long deadline = nanoTime.getAsLong() + capped * 1_000_000_000L;
    if (!set || deadline - until > 0) {
      until = deadline;
      set = true;
    }
  }

  /** Nanoseconds left, 0 when there is no hold or it has passed. */
  synchronized long remainingNanos() {
    if (!set) {
      return 0;
    }
    long left = until - nanoTime.getAsLong();
    return Math.max(left, 0);
  }
}
