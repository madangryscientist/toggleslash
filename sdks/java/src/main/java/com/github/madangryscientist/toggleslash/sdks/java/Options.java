package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Clock;
import java.time.Duration;
import java.util.concurrent.ThreadLocalRandom;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;

/** The options of {@link ToggleSlashClient#create}; make them with {@link #builder}. */
public final class Options {
  static final Duration DEFAULT_POLL_INTERVAL = Duration.ofSeconds(30);
  static final Duration DEFAULT_FLUSH_INTERVAL = Duration.ofSeconds(30);
  static final Duration DEFAULT_REQUEST_TIMEOUT = Duration.ofSeconds(10);
  static final Duration DEFAULT_WATCHDOG = Duration.ofSeconds(60);

  /** Evaluates a flag; the package's tests swap it to make an evaluation raise. */
  @FunctionalInterface
  interface Evaluate {
    EvalResult run(Ruleset ruleset, String flagKey, Object context);
  }

  private final String url;
  private final String sdkKey;
  private final boolean stream;
  private final Duration pollInterval;
  private final Duration flushInterval;
  private final Duration requestTimeout;
  private final Consumer<EvaluationEvent> afterEvaluation;
  private final Consumer<Throwable> onError;
  private final Clock clock;
  private final DoubleSupplier jitter;
  private final Duration watchdog;
  private final Evaluate evaluator;

  private Options(Builder b) {
    this.url = b.url == null ? "" : b.url;
    this.sdkKey = b.sdkKey == null ? "" : b.sdkKey;
    this.stream = b.stream == null || b.stream;
    this.pollInterval = positive(b.pollInterval, DEFAULT_POLL_INTERVAL);
    this.flushInterval = positive(b.flushInterval, DEFAULT_FLUSH_INTERVAL);
    this.requestTimeout = positive(b.requestTimeout, DEFAULT_REQUEST_TIMEOUT);
    this.afterEvaluation = b.afterEvaluation;
    this.onError = b.onError;
    this.clock = b.clock == null ? Clock.systemUTC() : b.clock;
    this.jitter =
        b.jitter == null ? () -> 0.5 + 0.5 * ThreadLocalRandom.current().nextDouble() : b.jitter;
    this.watchdog = positive(b.watchdog, DEFAULT_WATCHDOG);
    this.evaluator = b.evaluator == null ? Evaluator::evaluate : b.evaluator;
  }

  /** A duration that is missing, zero or negative takes the default (contract section 2). */
  private static Duration positive(Duration given, Duration fallback) {
    return given == null || given.isZero() || given.isNegative() ? fallback : given;
  }

  /**
   * Starts the options.
   *
   * @param url the server's base URL; trailing slashes are removed
   * @param sdkKey a server key ({@code fs_server_...})
   * @return a builder
   */
  public static Builder builder(String url, String sdkKey) {
    return new Builder(url, sdkKey);
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

  Duration pollInterval() {
    return pollInterval;
  }

  Duration flushInterval() {
    return flushInterval;
  }

  Duration requestTimeout() {
    return requestTimeout;
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

  Duration watchdog() {
    return watchdog;
  }

  Evaluate evaluator() {
    return evaluator;
  }

  /** Builds {@link Options}. A method that is not called leaves the default. */
  public static final class Builder {
    private final String url;
    private final String sdkKey;
    private Boolean stream;
    private Duration pollInterval;
    private Duration flushInterval;
    private Duration requestTimeout;
    private Consumer<EvaluationEvent> afterEvaluation;
    private Consumer<Throwable> onError;
    private Clock clock;
    private DoubleSupplier jitter;
    private Duration watchdog;
    private Evaluate evaluator;

    private Builder(String url, String sdkKey) {
      this.url = url;
      this.sdkKey = sdkKey;
    }

    /**
     * Follows the stream, or only polls.
     *
     * @param on true (the default) follows {@code GET /sdk/v1/stream}
     * @return this builder
     */
    public Builder stream(boolean on) {
      this.stream = on;
      return this;
    }

    /**
     * How often to poll when the stream is down or off, and the longest reconnect wait.
     *
     * @param value the interval (default 30 s)
     * @return this builder
     */
    public Builder pollInterval(Duration value) {
      this.pollInterval = value;
      return this;
    }

    /**
     * How often usage counts are sent.
     *
     * @param value the interval (default 30 s)
     * @return this builder
     */
    public Builder flushInterval(Duration value) {
      this.flushInterval = value;
      return this;
    }

    /**
     * The longest any one request may take, body included.
     *
     * @param value the limit (default 10 s)
     * @return this builder
     */
    public Builder requestTimeout(Duration value) {
      this.requestTimeout = value;
      return this;
    }

    /**
     * Called after every evaluation, on the caller's thread, before the call returns.
     *
     * @param hook the hook; an exception it throws goes to {@code onError}
     * @return this builder
     */
    public Builder afterEvaluation(Consumer<EvaluationEvent> hook) {
      this.afterEvaluation = hook;
      return this;
    }

    /**
     * Told about every failure the SDK absorbs. An exception it throws is ignored.
     *
     * @param handler the handler
     * @return this builder
     */
    public Builder onError(Consumer<Throwable> handler) {
      this.onError = handler;
      return this;
    }

    Builder clock(Clock value) {
      this.clock = value;
      return this;
    }

    /** The reconnect wait's multiplier, uniform in [0.5, 1) by default; a test fixes it. */
    Builder jitter(DoubleSupplier value) {
      this.jitter = value;
      return this;
    }

    Builder watchdog(Duration value) {
      this.watchdog = value;
      return this;
    }

    Builder evaluator(Evaluate value) {
      this.evaluator = value;
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
