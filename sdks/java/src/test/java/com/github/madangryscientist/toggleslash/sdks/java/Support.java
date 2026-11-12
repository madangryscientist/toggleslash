package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Duration;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.function.BooleanSupplier;

/** Small helpers the tests share. */
final class Support {
  static final String KEY = "fs_server_test_key_0123456789";

  private Support() {}

  /** Options for a polling client against the fake server. */
  static Options.Builder base(FakeServer server) {
    return Options.builder().url(server.url()).sdkKey(KEY).stream(false)
        .pollInterval(Duration.ofMillis(100))
        .flushInterval(Duration.ofSeconds(60))
        .requestTimeout(Duration.ofSeconds(5));
  }

  static boolean until(long millis, BooleanSupplier condition) throws InterruptedException {
    long end = System.nanoTime() + millis * 1_000_000L;
    while (System.nanoTime() < end) {
      if (condition.getAsBoolean()) {
        return true;
      }
      Thread.sleep(5);
    }
    return condition.getAsBoolean();
  }

  static Context user(String key) {
    return Context.user(key);
  }

  /** An error list a test passes as {@code onError}. */
  static List<Throwable> errors() {
    return new CopyOnWriteArrayList<>();
  }
}
