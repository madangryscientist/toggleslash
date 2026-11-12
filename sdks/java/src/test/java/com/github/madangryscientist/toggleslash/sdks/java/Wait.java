package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.function.BooleanSupplier;

/**
 * Polls a condition against a deadline, so a test waits for an effect rather than a guessed time.
 */
final class Wait {
  private Wait() {}

  static boolean until(long timeoutMs, BooleanSupplier condition) {
    long end = System.nanoTime() + timeoutMs * 1_000_000L;
    while (System.nanoTime() < end) {
      if (condition.getAsBoolean()) {
        return true;
      }
      try {
        Thread.sleep(5);
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
        return false;
      }
    }
    return condition.getAsBoolean();
  }
}
