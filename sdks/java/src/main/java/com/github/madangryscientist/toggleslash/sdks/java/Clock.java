package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * The SDK's time source and its wait. Production uses {@link #SYSTEM}; the package's tests pass
 * their own through a package-private option of {@link ClientOptions}, so no global and no
 * environment variable is read.
 */
interface Clock {

  /** The real clock. */
  Clock SYSTEM =
      new Clock() {
        @Override
        public long wallMillis() {
          return System.currentTimeMillis();
        }

        @Override
        public long nanos() {
          return System.nanoTime();
        }

        @Override
        public void await(Sleeper sleeper, long nanos) throws InterruptedException {
          sleeper.pause(nanos);
        }
      };

  /** Wall-clock time in epoch milliseconds: the UTC day of a count and {@code lastSyncAt}. */
  long wallMillis();

  /** A monotonic reading in nanoseconds: waits and {@code Retry-After} holds. */
  long nanos();

  /**
   * Waits up to {@code nanos} on {@code sleeper}, returning early when it is woken.
   *
   * @throws InterruptedException when the thread is interrupted
   */
  void await(Sleeper sleeper, long nanos) throws InterruptedException;
}
