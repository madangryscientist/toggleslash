package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicLong;

/**
 * A test clock. The wall time is set by the test. A thread named in {@code jumping} does not sleep:
 * each of its waits is recorded and the monotonic clock jumps forward by it, so a 300-second hold
 * passes in milliseconds. Every other wait is real.
 */
final class FakeClock implements Clock {

  final AtomicLong wall = new AtomicLong(1_700_000_000_000L);
  private final AtomicLong jumped = new AtomicLong();
  final List<Long> syncWaits = new CopyOnWriteArrayList<>();
  final List<Long> flushWaits = new CopyOnWriteArrayList<>();
  private final boolean jumpSync;
  private final boolean jumpFlush;
  volatile boolean wallThrows;

  FakeClock(boolean jumpSync, boolean jumpFlush) {
    this.jumpSync = jumpSync;
    this.jumpFlush = jumpFlush;
  }

  @Override
  public long wallMillis() {
    if (wallThrows) {
      throw new IllegalStateException("the clock is broken");
    }
    return wall.get();
  }

  @Override
  public long nanos() {
    return System.nanoTime() + jumped.get();
  }

  @Override
  public void await(Sleeper sleeper, long nanos) throws InterruptedException {
    String name = Thread.currentThread().getName();
    boolean sync = name.equals("toggleslash-sync");
    boolean flush = name.equals("toggleslash-flush");
    if ((sync && jumpSync) || (flush && jumpFlush)) {
      (sync ? syncWaits : flushWaits).add(nanos);
      jumped.addAndGet(nanos);
      Thread.sleep(1);
      return;
    }
    sleeper.pause(nanos);
  }
}
