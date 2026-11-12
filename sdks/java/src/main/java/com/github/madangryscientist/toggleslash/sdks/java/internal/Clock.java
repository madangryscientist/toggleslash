package com.github.madangryscientist.toggleslash.sdks.java.internal;

/** The two clocks the SDK reads, so a test can move wall time. */
public interface Clock {
  /** The system clock. */
  Clock SYSTEM =
      new Clock() {
        @Override
        public long nowMillis() {
          return System.currentTimeMillis();
        }

        @Override
        public long monoNanos() {
          return System.nanoTime();
        }
      };

  /**
   * Wall time.
   *
   * @return milliseconds since the epoch
   */
  long nowMillis();

  /**
   * A monotonic time, for waits and holds.
   *
   * @return nanoseconds from an arbitrary origin
   */
  long monoNanos();
}
