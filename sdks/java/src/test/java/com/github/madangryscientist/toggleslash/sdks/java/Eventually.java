package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.function.BooleanSupplier;

/**
 * Waits for a condition on real time, with a wide limit, since a test must not depend on a
 * machine's speed.
 */
final class Eventually {
  private Eventually() {}

  /** Waits up to {@code limitMillis} for {@code condition}; true if it held. */
  static boolean holds(long limitMillis, BooleanSupplier condition) {
    long deadline = System.nanoTime() + limitMillis * 1_000_000L;
    while (System.nanoTime() < deadline) {
      if (condition.getAsBoolean()) {
        return true;
      }
      try {
        Thread.sleep(10);
      } catch (InterruptedException interrupted) {
        Thread.currentThread().interrupt();
        return condition.getAsBoolean();
      }
    }
    return condition.getAsBoolean();
  }
}
