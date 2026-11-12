package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.concurrent.atomic.AtomicLong;

/** A wait the server asked for with Retry-After. It only ever lengthens. */
final class Hold {
  /** The longest wait honoured, in milliseconds. */
  static final long CAP_MS = 300_000;

  private final AtomicLong deadline = new AtomicLong(System.nanoTime());

  void extendMs(long ms) {
    long wanted = System.nanoTime() + Math.min(ms, CAP_MS) * 1_000_000L;
    deadline.accumulateAndGet(
        wanted, (current, candidate) -> candidate - current > 0 ? candidate : current);
  }

  /** Milliseconds left, rounded up; zero when no hold is in force. */
  long remainingMs() {
    long left = deadline.get() - System.nanoTime();
    return left <= 0 ? 0 : (left + 999_999) / 1_000_000;
  }
}
