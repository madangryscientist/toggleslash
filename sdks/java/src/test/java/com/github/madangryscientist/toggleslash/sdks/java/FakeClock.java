package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * A clock the test moves by hand. {@code sleep} records the wait it was asked for and then blocks
 * until the client closes, so a loop under test idles and the test decides what happens next.
 */
final class FakeClock implements Clock {
  /** One wait the SDK asked for. */
  record Sleep(String thread, long millis) {}

  private volatile long wall;
  private volatile long mono;
  final List<Sleep> sleeps = Collections.synchronizedList(new ArrayList<>());

  FakeClock(long wall) {
    this.wall = wall;
  }

  void setWall(long value) {
    wall = value;
  }

  void setMono(long value) {
    mono = value;
  }

  @Override
  public long wallMillis() {
    return wall;
  }

  @Override
  public long monoMillis() {
    return mono;
  }

  @Override
  public boolean sleep(long millis, Closing closing) throws InterruptedException {
    sleeps.add(new Sleep(Thread.currentThread().getName(), millis));
    return closing.await(Long.MAX_VALUE / 4_000_000L);
  }

  /** The waits one thread asked for. */
  List<Long> sleepsOf(String thread) {
    List<Long> found = new ArrayList<>();
    synchronized (sleeps) {
      for (Sleep sleep : sleeps) {
        if (sleep.thread().equals(thread)) {
          found.add(sleep.millis());
        }
      }
    }
    return found;
  }
}
