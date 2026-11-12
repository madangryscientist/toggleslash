package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * The SDK's waiting and its monotonic time, in one place so a test can replace both. A wait ends at
 * once when {@code close} counts the latch down.
 */
interface Pacer {
  /**
   * The monotonic time.
   *
   * @return nanoseconds on a clock that only moves forward
   */
  long nanoTime();

  /**
   * Waits.
   *
   * @param closed the latch {@code close} counts down
   * @param nanos how long to wait
   * @throws InterruptedException when the thread is interrupted
   */
  void sleep(CountDownLatch closed, long nanos) throws InterruptedException;

  /** The real one. */
  Pacer SYSTEM =
      new Pacer() {
        @Override
        public long nanoTime() {
          return System.nanoTime();
        }

        @Override
        public void sleep(CountDownLatch closed, long nanos) throws InterruptedException {
          if (nanos > 0) {
            closed.await(nanos, TimeUnit.NANOSECONDS);
          }
        }
      };
}
