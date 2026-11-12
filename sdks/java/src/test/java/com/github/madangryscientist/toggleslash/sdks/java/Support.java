package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.time.ZoneId;
import java.time.ZoneOffset;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.BooleanSupplier;

/** Small helpers the tests share. */
final class Support {
  private Support() {}

  static Duration ms(long value) {
    return Duration.ofMillis(value);
  }

  static Options options(FakeServer server) {
    return new Options().url(server.url()).sdkKey("fs_server_testkey").stream(false);
  }

  /** Waits until the condition holds, failing the test after {@code limitMs}. */
  static void eventually(String what, long limitMs, BooleanSupplier condition) {
    long deadline = System.nanoTime() + limitMs * 1_000_000L;
    while (!condition.getAsBoolean()) {
      if (System.nanoTime() > deadline) {
        throw new AssertionError("timed out waiting for " + what);
      }
      try {
        Thread.sleep(5);
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
        throw new AssertionError("interrupted waiting for " + what);
      }
    }
  }

  static String ruleset(String... flags) {
    return "{\"flags\":[" + String.join(",", flags) + "],\"segments\":[]}";
  }

  /** A flag that is on and serves its one variation to everyone. */
  static String flag(String key, String valueJson, int version) {
    return "{\"key\":\""
        + key
        + "\",\"variations\":["
        + valueJson
        + "],\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":[],"
        + "\"fallthrough\":{\"variation\":0},\"salt\":\"s\",\"version\":"
        + version
        + "}";
  }

  /** A clock the test moves by hand. */
  static final class MutableClock extends Clock {
    private final AtomicLong millis;

    MutableClock(Instant start) {
      this.millis = new AtomicLong(start.toEpochMilli());
    }

    void set(Instant value) {
      millis.set(value.toEpochMilli());
    }

    @Override
    public ZoneId getZone() {
      return ZoneOffset.UTC;
    }

    @Override
    public Clock withZone(ZoneId zone) {
      return this;
    }

    @Override
    public Instant instant() {
      return Instant.ofEpochMilli(millis.get());
    }
  }
}
