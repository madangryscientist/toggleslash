package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/** A signal that a waiting loop can sleep on and that wakes it when it is raised. */
final class Stop {
  private final CountDownLatch latch = new CountDownLatch(1);
  private final List<Runnable> listeners = new CopyOnWriteArrayList<>();

  void signal() {
    latch.countDown();
    for (Runnable listener : listeners) {
      listener.run();
    }
  }

  boolean isSignalled() {
    return latch.getCount() == 0;
  }

  /** Waits up to {@code millis} real milliseconds; true when the signal was raised. */
  boolean await(long millis) throws InterruptedException {
    return latch.await(Math.max(0, millis), TimeUnit.MILLISECONDS);
  }

  void onSignal(Runnable listener) {
    listeners.add(listener);
  }

  void removeListener(Runnable listener) {
    listeners.remove(listener);
  }
}
