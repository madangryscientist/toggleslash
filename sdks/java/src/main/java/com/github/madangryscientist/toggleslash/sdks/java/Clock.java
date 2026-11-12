package com.github.madangryscientist.toggleslash.sdks.java;

/** Time as the client sees it, so a test can move it by hand instead of waiting. */
interface Clock {
  /** Milliseconds since the Unix epoch. */
  long now();

  /**
   * Waits {@code millis} of this clock's time, or until {@code stop} is raised.
   *
   * @return true when the time passed, false when the stop was raised first
   */
  boolean sleep(long millis, Stop stop) throws InterruptedException;

  /** The real clock. */
  Clock SYSTEM =
      new Clock() {
        @Override
        public long now() {
          return System.currentTimeMillis();
        }

        @Override
        public boolean sleep(long millis, Stop stop) throws InterruptedException {
          if (millis <= 0) {
            return !stop.isSignalled();
          }
          return !stop.await(millis);
        }
      };
}
