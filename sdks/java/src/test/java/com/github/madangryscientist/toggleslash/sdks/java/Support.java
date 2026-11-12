package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.concurrent.atomic.AtomicLong;
import java.util.function.BooleanSupplier;

/** Small helpers the native tests share. */
final class Support {
  private Support() {}

  /** Polls {@code condition} until it holds or {@code millis} pass; true if it held. */
  static boolean waitFor(BooleanSupplier condition, long millis) throws InterruptedException {
    long end = System.nanoTime() + millis * 1_000_000L;
    while (System.nanoTime() < end) {
      if (condition.getAsBoolean()) {
        return true;
      }
      Thread.sleep(5);
    }
    return condition.getAsBoolean();
  }

  /** A clock that moves only when the test moves it; sleeping waits in real time for the stop. */
  static final class FakeClock implements Clock {
    private final AtomicLong now;

    FakeClock(long start) {
      this.now = new AtomicLong(start);
    }

    void set(long value) {
      now.set(value);
    }

    @Override
    public long now() {
      return now.get();
    }

    @Override
    public boolean sleep(long millis, Stop stop) throws InterruptedException {
      return !stop.await(Math.max(millis, 0));
    }
  }
}
