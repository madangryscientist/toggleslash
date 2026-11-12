package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * A clock a test moves by hand: {@code sleep} returns only when the time has been advanced past it,
 * or the stop latch is counted down. Step it only when the SDK is waiting on it ({@link
 * #awaitSleepers}).
 */
final class TestClock implements Clock {
  private final Object lock = new Object();
  private long now;
  private final AtomicInteger sleepers = new AtomicInteger();
  private volatile long longestWait;
  private final List<Long> waits = new CopyOnWriteArrayList<>();

  TestClock(long start) {
    this.now = start;
  }

  @Override
  public long now() {
    synchronized (lock) {
      return now;
    }
  }

  @Override
  public boolean sleep(long millis, CountDownLatch stop) throws InterruptedException {
    long until;
    synchronized (lock) {
      until = now + millis;
    }
    longestWait = Math.max(longestWait, millis);
    waits.add(millis);
    sleepers.incrementAndGet();
    try {
      while (true) {
        if (stop.getCount() == 0) {
          return true;
        }
        synchronized (lock) {
          if (now >= until) {
            return false;
          }
          lock.wait(5);
        }
      }
    } finally {
      sleepers.decrementAndGet();
    }
  }

  void advance(long millis) {
    synchronized (lock) {
      now += millis;
      lock.notifyAll();
    }
  }

  void set(long value) {
    synchronized (lock) {
      now = value;
      lock.notifyAll();
    }
  }

  /** Every wait asked for, in order. */
  List<Long> waits() {
    return waits;
  }

  /** The longest single wait any sleeper has asked for. */
  long longestWait() {
    return longestWait;
  }

  /** Waits until {@code n} threads are asleep on this clock. */
  boolean awaitSleepers(int n, long millis) throws InterruptedException {
    long end = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(millis);
    while (System.nanoTime() < end) {
      if (sleepers.get() >= n) {
        return true;
      }
      Thread.sleep(2);
    }
    return sleepers.get() >= n;
  }
}
