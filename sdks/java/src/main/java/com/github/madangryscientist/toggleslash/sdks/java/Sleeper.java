package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * A wait that {@code close} can end. Once woken it stays woken: every later wait returns at once.
 * Its monitor is held only to read or set the flag and to wait, never across I/O or a callback.
 */
final class Sleeper {

  private boolean woken;

  /** Ends every wait, now and later. */
  synchronized void wake() {
    woken = true;
    notifyAll();
  }

  synchronized boolean isWoken() {
    return woken;
  }

  /**
   * Waits up to {@code nanos} for {@link #wake}; returns whether it was woken.
   *
   * @throws InterruptedException when the thread is interrupted
   */
  synchronized boolean pause(long nanos) throws InterruptedException {
    long deadline = System.nanoTime() + Math.min(nanos, Long.MAX_VALUE / 2);
    while (!woken) {
      long left = deadline - System.nanoTime();
      if (left <= 0) {
        break;
      }
      long millis = left / 1_000_000L;
      wait(millis, (int) (left % 1_000_000L));
    }
    return woken;
  }
}
