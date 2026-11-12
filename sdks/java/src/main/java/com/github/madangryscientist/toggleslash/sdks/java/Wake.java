package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.concurrent.TimeUnit;

/**
 * A sleep that {@code close} can cut short, without interrupting a thread that is in the middle of
 * a request.
 */
final class Wake {
  private final Object monitor = new Object();
  private boolean woken;

  /**
   * Sleeps until {@code deadlineNanos} (a {@code System.nanoTime} reading) or until {@link
   * #wakeAll()}.
   */
  void sleepUntil(long deadlineNanos) {
    synchronized (monitor) {
      while (!woken) {
        long remaining = deadlineNanos - System.nanoTime();
        if (remaining <= 0) {
          return;
        }
        try {
          TimeUnit.NANOSECONDS.timedWait(monitor, remaining);
        } catch (InterruptedException interrupted) {
          return;
        }
      }
    }
  }

  /** Cuts every sleep short, now and later. */
  void wakeAll() {
    synchronized (monitor) {
      woken = true;
      monitor.notifyAll();
    }
  }

  boolean isWoken() {
    synchronized (monitor) {
      return woken;
    }
  }
}
