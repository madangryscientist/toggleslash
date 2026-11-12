package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Duration;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.BooleanSupplier;

/** Helpers the tests share. */
final class Support {
  /** A clock the test moves. */
  static final class MutableClock extends java.time.Clock {
    private volatile java.time.Instant now;

    MutableClock(java.time.Instant now) {
      this.now = now;
    }

    void set(java.time.Instant value) {
      this.now = value;
    }

    @Override
    public java.time.ZoneId getZone() {
      return java.time.ZoneOffset.UTC;
    }

    @Override
    public java.time.Clock withZone(java.time.ZoneId zone) {
      return this;
    }

    @Override
    public java.time.Instant instant() {
      return now;
    }
  }

  private Support() {}

  /** A ruleset of the given flags, each {@code key} with a boolean value and a version. */
  static String ruleset(String... flags) {
    StringBuilder json = new StringBuilder("{\"flags\":[");
    for (int i = 0; i < flags.length; i++) {
      json.append(i == 0 ? "" : ",").append(flags[i]);
    }
    return json.append("],\"segments\":[]}").toString();
  }

  /** A flag with one served value (variation 1 by fallthrough). */
  static String flag(String key, String value, long version) {
    return "{\"key\":\""
        + key
        + "\",\"variations\":[null,"
        + value
        + "],\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],"
        + "\"fallthrough\":{\"variation\":1},\"salt\":\"s\",\"version\":"
        + version
        + "}";
  }

  static String boolFlags() {
    return ruleset(flag("f", "true", 7));
  }

  static Options.Builder options(FakeServer server) {
    return Options.builder(server.url(), "test-key")
        .requestTimeout(Duration.ofSeconds(5))
        .pollInterval(Duration.ofSeconds(60))
        .flushInterval(Duration.ofSeconds(600));
  }

  /** Polls until the condition holds, or fails after {@code millis}. */
  static void await(String what, long millis, BooleanSupplier condition)
      throws InterruptedException, TimeoutException {
    long deadline = System.nanoTime() + millis * 1_000_000L;
    while (!condition.getAsBoolean()) {
      if (System.nanoTime() > deadline) {
        throw new TimeoutException("timed out waiting for " + what);
      }
      Thread.sleep(5);
    }
  }

  /**
   * A clock the tests set; the monotonic time follows it and only the sync thread's waits are
   * skipped.
   */
  static final class TestPacer implements Pacer {
    final AtomicLong now = new AtomicLong(1_000_000_000_000L);
    final java.util.List<Long> sleeps = new java.util.concurrent.CopyOnWriteArrayList<>();
    private final boolean skipSyncWaits;

    TestPacer(boolean skipSyncWaits) {
      this.skipSyncWaits = skipSyncWaits;
    }

    @Override
    public long nanoTime() {
      return now.get();
    }

    @Override
    public void sleep(CountDownLatch closed, long nanos) throws InterruptedException {
      if (skipSyncWaits && Thread.currentThread().getName().equals("toggleslash-sync")) {
        sleeps.add(nanos);
        if (closed.await(2, java.util.concurrent.TimeUnit.MILLISECONDS)) {
          return;
        }
        now.addAndGet(nanos);
        return;
      }
      closed.await(nanos, java.util.concurrent.TimeUnit.NANOSECONDS);
    }

    void advance(Duration by) {
      now.addAndGet(by.toNanos());
    }
  }
}
