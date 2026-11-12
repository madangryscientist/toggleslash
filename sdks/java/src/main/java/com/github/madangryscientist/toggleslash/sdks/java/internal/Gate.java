package com.github.madangryscientist.toggleslash.sdks.java.internal;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/** A stop signal that wakes every sleeper when it opens. */
public final class Gate {
  private final CountDownLatch latch = new CountDownLatch(1);

  /** Opens the gate for good. */
  public void open() {
    latch.countDown();
  }

  /**
   * Whether the gate is open.
   *
   * @return true once {@link #open()} was called
   */
  public boolean isOpen() {
    return latch.getCount() == 0;
  }

  /**
   * Sleeps until the time passes or the gate opens.
   *
   * @param millis how long to sleep at most
   * @return true if the gate is open
   */
  public boolean sleep(long millis) {
    if (isOpen()) {
      return true;
    }
    try {
      return millis <= 0 ? isOpen() : latch.await(millis, TimeUnit.MILLISECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return true;
    }
  }
}
