package com.github.madangryscientist.toggleslash.sdks.java;

/** The close flag, and the monitor every wait of the SDK's threads is on. */
final class Closing {
  private final Object lock = new Object();
  private volatile boolean closed;

  boolean isClosed() {
    return closed;
  }

  void close() {
    synchronized (lock) {
      closed = true;
      lock.notifyAll();
    }
  }

  /** Waits up to the time or until closed; true when closed. */
  boolean await(long millis) throws InterruptedException {
    synchronized (lock) {
      long end = System.nanoTime() + millis * 1_000_000L;
      while (!closed) {
        long left = (end - System.nanoTime()) / 1_000_000L;
        if (left <= 0) {
          break;
        }
        lock.wait(left);
      }
      return closed;
    }
  }
}
