package com.github.madangryscientist.toggleslash.sdks.java;

/** Time and waiting, as a seam for tests. */
interface Clock {
  /** The system clock. */
  Clock SYSTEM =
      new Clock() {
        @Override
        public long wallMillis() {
          return System.currentTimeMillis();
        }

        @Override
        public long monoMillis() {
          return System.nanoTime() / 1_000_000L;
        }

        @Override
        public boolean sleep(long millis, Closing closing) throws InterruptedException {
          return closing.await(millis);
        }
      };

  /**
   * Wall time, for the UTC day and {@code lastSyncAt}.
   *
   * @return epoch milliseconds
   */
  long wallMillis();

  /**
   * A monotonic time, for holds.
   *
   * @return milliseconds
   */
  long monoMillis();

  /**
   * Waits up to the time, or until close.
   *
   * @param millis how long
   * @param closing the close flag
   * @return true when the client is closing
   * @throws InterruptedException when interrupted
   */
  boolean sleep(long millis, Closing closing) throws InterruptedException;
}
