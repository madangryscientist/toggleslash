package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Evaluator;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.time.Clock;
import java.time.Duration;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;
import javax.net.ssl.SSLContext;

/**
 * The options of a {@link Client}, built with {@link #builder(String, String)}. A field that was
 * not given is null in the builder, so each default is applied in one place and {@code
 * stream(false)} differs from not calling {@code stream}.
 */
public final class Options {
  /** The evaluation the client runs, so a test can make it raise. */
  @FunctionalInterface
  interface Evaluate {
    Result apply(Ruleset ruleset, String key, Object context);
  }

  private static final Duration DEFAULT_POLL = Duration.ofSeconds(30);
  private static final Duration DEFAULT_FLUSH = Duration.ofSeconds(30);
  private static final Duration DEFAULT_TIMEOUT = Duration.ofSeconds(10);
  private static final Duration DEFAULT_WATCHDOG = Duration.ofSeconds(60);
  private static final long LONGEST_NANOS = 1L << 61;

  private final String url;
  private final String sdkKey;
  private final boolean stream;
  private final long pollNanos;
  private final long flushNanos;
  private final long timeoutNanos;
  private final long watchdogNanos;
  private final Consumer<EvaluationEvent> afterEvaluation;
  private final Consumer<Throwable> onError;
  private final Clock clock;
  private final DoubleSupplier jitter;
  private final Pacer pacer;
  private final SSLContext sslContext;
  private final Evaluate evaluate;

  private Options(Builder b) {
    this.url = stripSlashes(b.url == null ? "" : b.url);
    this.sdkKey = b.sdkKey == null ? "" : b.sdkKey;
    this.stream = b.stream == null || b.stream;
    this.pollNanos = nanos(b.pollInterval, DEFAULT_POLL);
    this.flushNanos = nanos(b.flushInterval, DEFAULT_FLUSH);
    this.timeoutNanos = nanos(b.requestTimeout, DEFAULT_TIMEOUT);
    this.watchdogNanos = nanos(b.watchdog, DEFAULT_WATCHDOG);
    this.afterEvaluation = b.afterEvaluation;
    this.onError = b.onError;
    this.clock = b.clock == null ? Clock.systemUTC() : b.clock;
    this.jitter =
        b.jitter == null
            ? () -> java.util.concurrent.ThreadLocalRandom.current().nextDouble()
            : b.jitter;
    this.pacer = b.pacer == null ? Pacer.SYSTEM : b.pacer;
    this.sslContext = b.sslContext;
    this.evaluate = b.evaluate == null ? Evaluator::evaluate : b.evaluate;
  }

  /**
   * Starts the options.
   *
   * @param url the server's base URL; trailing {@code /} characters are removed
   * @param sdkKey a server key
   * @return a builder
   */
  public static Builder builder(String url, String sdkKey) {
    return new Builder(url, sdkKey);
  }

  private static String stripSlashes(String text) {
    int end = text.length();
    while (end > 0 && text.charAt(end - 1) == '/') {
      end--;
    }
    return text.substring(0, end);
  }

  /** A duration in nanoseconds; null, zero and negative fall back to the default. */
  private static long nanos(Duration given, Duration fallback) {
    Duration use = given == null || given.isZero() || given.isNegative() ? fallback : given;
    try {
      return Math.min(use.toNanos(), LONGEST_NANOS);
    } catch (ArithmeticException e) {
      return LONGEST_NANOS;
    }
  }

  String url() {
    return url;
  }

  String sdkKey() {
    return sdkKey;
  }

  boolean stream() {
    return stream;
  }

  long pollNanos() {
    return pollNanos;
  }

  long flushNanos() {
    return flushNanos;
  }

  long timeoutNanos() {
    return timeoutNanos;
  }

  long watchdogNanos() {
    return watchdogNanos;
  }

  Consumer<EvaluationEvent> afterEvaluation() {
    return afterEvaluation;
  }

  Consumer<Throwable> onError() {
    return onError;
  }

  Clock clock() {
    return clock;
  }

  DoubleSupplier jitter() {
    return jitter;
  }

  Pacer pacer() {
    return pacer;
  }

  SSLContext sslContext() {
    return sslContext;
  }

  Evaluate evaluate() {
    return evaluate;
  }

  /** Builds {@link Options}. Every setter returns the builder. */
  public static final class Builder {
    private final String url;
    private final String sdkKey;
    private Boolean stream;
    private Duration pollInterval;
    private Duration flushInterval;
    private Duration requestTimeout;
    private Duration watchdog;
    private Consumer<EvaluationEvent> afterEvaluation;
    private Consumer<Throwable> onError;
    private Clock clock;
    private DoubleSupplier jitter;
    private Pacer pacer;
    private SSLContext sslContext;
    private Evaluate evaluate;

    private Builder(String url, String sdkKey) {
      this.url = url;
      this.sdkKey = sdkKey;
    }

    /**
     * Follows {@code GET /sdk/v1/stream} (the default) or polls only.
     *
     * @param on whether to stream
     * @return this
     */
    public Builder stream(boolean on) {
      this.stream = on;
      return this;
    }

    /**
     * How often to poll when the stream is down or off, and the longest reconnect wait. Default 30
     * s; null, zero or negative gives the default.
     *
     * @param value the interval
     * @return this
     */
    public Builder pollInterval(Duration value) {
      this.pollInterval = value;
      return this;
    }

    /**
     * How often usage counts are sent. Default 30 s; null, zero or negative gives the default.
     *
     * @param value the interval
     * @return this
     */
    public Builder flushInterval(Duration value) {
      this.flushInterval = value;
      return this;
    }

    /**
     * The longest any one request may take, body included. Default 10 s; null, zero or negative
     * gives the default.
     *
     * @param value the limit
     * @return this
     */
    public Builder requestTimeout(Duration value) {
      this.requestTimeout = value;
      return this;
    }

    /**
     * Called after every {@code variation} and {@code variationDetail}, on the caller's thread,
     * before the call returns. An exception it throws goes to {@code onError}.
     *
     * @param hook the hook
     * @return this
     */
    public Builder afterEvaluation(Consumer<EvaluationEvent> hook) {
      this.afterEvaluation = hook;
      return this;
    }

    /**
     * Told about every failure the SDK absorbs. Not given ignores. An exception it throws is
     * ignored.
     *
     * @param handler the handler
     * @return this
     */
    public Builder onError(Consumer<Throwable> handler) {
      this.onError = handler;
      return this;
    }

    Builder watchdog(Duration value) {
      this.watchdog = value;
      return this;
    }

    Builder clock(Clock value) {
      this.clock = value;
      return this;
    }

    Builder jitter(DoubleSupplier value) {
      this.jitter = value;
      return this;
    }

    Builder pacer(Pacer value) {
      this.pacer = value;
      return this;
    }

    Builder evaluate(Evaluate value) {
      this.evaluate = value;
      return this;
    }

    Builder sslContext(SSLContext value) {
      this.sslContext = value;
      return this;
    }

    /**
     * Builds the options.
     *
     * @return the options
     */
    public Options build() {
      return new Options(this);
    }
  }
}
