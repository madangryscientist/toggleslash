package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/** Time as the SDK sees it, a seam for tests: they move it by hand instead of waiting. */
public interface Clock {
  /** The real clock. */
  Clock SYSTEM =
      new Clock() {
        @Override
        public long now() {
          return System.currentTimeMillis();
        }

        @Override
        public boolean sleep(long millis, CountDownLatch stop) throws InterruptedException {
          return stop.await(millis, TimeUnit.MILLISECONDS);
        }
      };

  /** Milliseconds since the Unix epoch. */
  long now();

  /**
   * Waits {@code millis}, or until {@code stop} is counted down.
   *
   * @return true when {@code stop} ended the wait
   */
  boolean sleep(long millis, CountDownLatch stop) throws InterruptedException;
}
