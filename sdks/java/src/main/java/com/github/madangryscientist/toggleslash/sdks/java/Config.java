package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Clock;
import java.time.Duration;
import java.util.concurrent.ThreadLocalRandom;
import java.util.function.DoubleSupplier;
import javax.net.ssl.SSLContext;

/** The options after defaults, read once at create. */
record Config(
    String baseUrl,
    String sdkKey,
    boolean stream,
    long pollMs,
    long flushMs,
    long timeoutMs,
    long idleMs,
    Clock clock,
    DoubleSupplier random,
    SSLContext sslContext) {
  static final long DEFAULT_INTERVAL_MS = 30_000;
  static final long DEFAULT_TIMEOUT_MS = 10_000;
  static final long DEFAULT_IDLE_MS = 60_000;

  static Config of(Options o) {
    String url = o.url == null ? "" : o.url;
    int end = url.length();
    while (end > 0 && url.charAt(end - 1) == '/') {
      end--;
    }
    return new Config(
        url.substring(0, end),
        o.sdkKey == null ? "" : o.sdkKey,
        o.stream == null || o.stream,
        millis(o.pollInterval, DEFAULT_INTERVAL_MS),
        millis(o.flushInterval, DEFAULT_INTERVAL_MS),
        millis(o.requestTimeout, DEFAULT_TIMEOUT_MS),
        millis(o.streamIdle, DEFAULT_IDLE_MS),
        o.clock == null ? Clock.systemUTC() : o.clock,
        o.random == null ? () -> ThreadLocalRandom.current().nextDouble() : o.random,
        o.sslContext);
  }

  /**
   * A duration in milliseconds; none, zero or negative is the default, and a huge one is clamped.
   */
  private static long millis(Duration value, long fallback) {
    if (value == null || value.isZero() || value.isNegative()) {
      return fallback;
    }
    try {
      return value.toMillis() == 0 ? 1 : value.toMillis();
    } catch (ArithmeticException e) {
      return Long.MAX_VALUE / 4;
    }
  }
}
