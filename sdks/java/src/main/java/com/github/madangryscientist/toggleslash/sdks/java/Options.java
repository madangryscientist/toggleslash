package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.time.Clock;
import java.time.Duration;
import java.util.concurrent.ThreadLocalRandom;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;
import java.util.function.LongSupplier;

/**
 * What {@link Client#create(Options)} reads. Build one with {@link #builder(String, String)}. A
 * duration that is null, zero or negative is the default.
 */
public final class Options {
  static final Duration DEFAULT_POLL_INTERVAL = Duration.ofSeconds(30);
  static final Duration DEFAULT_FLUSH_INTERVAL = Duration.ofSeconds(30);
  static final Duration DEFAULT_REQUEST_TIMEOUT = Duration.ofSeconds(10);

  /** The server sends a keep-alive every 25 s; this long without a byte and the stream is dead. */
  static final Duration DEFAULT_STREAM_IDLE = Duration.ofSeconds(60);

  /** How one evaluation is done; a test seam, so a test can make it fail. */
  @FunctionalInterface
  interface Evaluator {
    Result evaluate(Ruleset ruleset, String flagKey, Object context);
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
  private final LongSupplier nanoTime;
  private final DoubleSupplier jitter;
  private final Duration streamIdle;
  private final Evaluator evaluator;

  private Options(Builder b) {
    this.url = b.url;
    this.sdkKey = b.sdkKey;
    this.stream = b.stream;
    this.pollInterval = positive(b.pollInterval, DEFAULT_POLL_INTERVAL);
    this.flushInterval = positive(b.flushInterval, DEFAULT_FLUSH_INTERVAL);
    this.requestTimeout = positive(b.requestTimeout, DEFAULT_REQUEST_TIMEOUT);
    this.afterEvaluation = b.afterEvaluation;
    this.onError = b.onError;
    this.clock = b.clock;
    this.nanoTime = b.nanoTime;
    this.jitter = b.jitter;
    this.streamIdle = positive(b.streamIdle, DEFAULT_STREAM_IDLE);
    this.evaluator = b.evaluator;
  }

  private static Duration positive(Duration value, Duration fallback) {
    return value == null || value.isZero() || value.isNegative() ? fallback : value;
  }

  /**
   * Starts options for a server at {@code url} (for example {@code https://flags.example.com}; a
   * trailing slash is dropped) and a server SDK key ({@code fs_server_...}).
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

  LongSupplier nanoTime() {
    return nanoTime;
  }

  DoubleSupplier jitter() {
    return jitter;
  }

  Duration streamIdle() {
    return streamIdle;
  }

  Evaluator evaluator() {
    return evaluator;
  }

  /** Builds {@link Options}. */
  public static final class Builder {
    private final String url;
    private final String sdkKey;
    private boolean stream = true;
    private Duration pollInterval;
    private Duration flushInterval;
    private Duration requestTimeout;
    private Consumer<EvaluationEvent> afterEvaluation;
    private Consumer<Throwable> onError;
    private Clock clock = Clock.systemUTC();
    private LongSupplier nanoTime = System::nanoTime;
    private DoubleSupplier jitter = () -> ThreadLocalRandom.current().nextDouble();
    private Duration streamIdle;
    private Evaluator evaluator = Ruleset::evaluate;

    private Builder(String url, String sdkKey) {
      this.url = url;
      this.sdkKey = sdkKey;
    }

    /** Follow the stream (default true). False polls only. */
    public Builder stream(boolean stream) {
      this.stream = stream;
      return this;
    }

    /** How often to poll when the stream is down or off, and the longest reconnect wait (30 s). */
    public Builder pollInterval(Duration pollInterval) {
      this.pollInterval = pollInterval;
      return this;
    }

    /** How often usage counts are sent (30 s). */
    public Builder flushInterval(Duration flushInterval) {
      this.flushInterval = flushInterval;
      return this;
    }

    /**
     * The longest any one request may take, and so the longest {@code ready} and {@code close} wait
     * (10 s).
     */
    public Builder requestTimeout(Duration requestTimeout) {
      this.requestTimeout = requestTimeout;
      return this;
    }

    /**
     * Called synchronously on the caller's thread after every evaluation, a fallback included. An
     * exception it throws goes to {@code onError} and never reaches the caller.
     */
    public Builder afterEvaluation(Consumer<EvaluationEvent> hook) {
      this.afterEvaluation = hook;
      return this;
    }

    /**
     * Told about every failure the SDK absorbs. It is never needed for correctness; an exception it
     * throws is ignored.
     */
    public Builder onError(Consumer<Throwable> onError) {
      this.onError = onError;
      return this;
    }

    Builder clock(Clock clock) {
      this.clock = clock;
      return this;
    }

    Builder nanoTime(LongSupplier nanoTime) {
      this.nanoTime = nanoTime;
      return this;
    }

    Builder jitter(DoubleSupplier jitter) {
      this.jitter = jitter;
      return this;
    }

    Builder streamIdle(Duration streamIdle) {
      this.streamIdle = streamIdle;
      return this;
    }

    Builder evaluator(Evaluator evaluator) {
      this.evaluator = evaluator;
      return this;
    }

    /** The options. */
    public Options build() {
      return new Options(this);
    }
  }
}
