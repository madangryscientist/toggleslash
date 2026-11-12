package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Evaluator;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.time.Clock;
import java.time.Duration;
import java.util.concurrent.ThreadLocalRandom;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;
import javax.net.ssl.SSLContext;

/**
 * The options of {@link Client#create(Options)}, made with {@link #builder()}.
 *
 * <p>A value that is not given, or is zero or negative for a duration, takes its default.
 */
public final class Options {
  static final long DEFAULT_INTERVAL_NANOS = Duration.ofSeconds(30).toNanos();
  static final long DEFAULT_TIMEOUT_NANOS = Duration.ofSeconds(10).toNanos();
  static final long DEFAULT_IDLE_NANOS = Duration.ofSeconds(60).toNanos();
  private static final long LONGEST_NANOS = Long.MAX_VALUE / 4;

  /** How a client evaluates; a test replaces it to make an evaluation raise. */
  @FunctionalInterface
  interface Evaluation {
    Result evaluate(Ruleset ruleset, String flagKey, Object context);
  }

  private final String url;
  private final String sdkKey;
  private final boolean stream;
  private final long pollNanos;
  private final long flushNanos;
  private final long requestNanos;
  private final Consumer<EvaluationEvent> afterEvaluation;
  private final Consumer<Throwable> onError;
  private final Clock clock;
  private final DoubleSupplier jitter;
  private final long idleNanos;
  private final Evaluation evaluation;
  private final SSLContext sslContext;

  private Options(Builder b) {
    this.url = stripSlashes(b.url == null ? "" : b.url);
    this.sdkKey = b.sdkKey == null ? "" : b.sdkKey;
    this.stream = b.stream == null || b.stream;
    this.pollNanos = nanos(b.pollInterval, DEFAULT_INTERVAL_NANOS);
    this.flushNanos = nanos(b.flushInterval, DEFAULT_INTERVAL_NANOS);
    this.requestNanos = nanos(b.requestTimeout, DEFAULT_TIMEOUT_NANOS);
    this.afterEvaluation = b.afterEvaluation;
    this.onError = b.onError;
    this.clock = b.clock == null ? Clock.systemUTC() : b.clock;
    this.jitter = b.jitter == null ? () -> ThreadLocalRandom.current().nextDouble() : b.jitter;
    this.idleNanos = nanos(b.streamIdleTimeout, DEFAULT_IDLE_NANOS);
    this.evaluation = b.evaluation == null ? Evaluator::evaluate : b.evaluation;
    this.sslContext = b.sslContext;
  }

  private static String stripSlashes(String url) {
    int end = url.length();
    while (end > 0 && url.charAt(end - 1) == '/') {
      end--;
    }
    return url.substring(0, end);
  }

  private static long nanos(Duration duration, long fallback) {
    if (duration == null || duration.isZero() || duration.isNegative()) {
      return fallback;
    }
    try {
      return Math.min(duration.toNanos(), LONGEST_NANOS);
    } catch (ArithmeticException tooLong) {
      return LONGEST_NANOS;
    }
  }

  /**
   * A new builder.
   *
   * @return the builder
   */
  public static Builder builder() {
    return new Builder();
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

  long requestNanos() {
    return requestNanos;
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

  long idleNanos() {
    return idleNanos;
  }

  Evaluation evaluation() {
    return evaluation;
  }

  SSLContext sslContext() {
    return sslContext;
  }

  /** Builds {@link Options}. */
  public static final class Builder {
    private String url;
    private String sdkKey;
    private Boolean stream;
    private Duration pollInterval;
    private Duration flushInterval;
    private Duration requestTimeout;
    private Consumer<EvaluationEvent> afterEvaluation;
    private Consumer<Throwable> onError;
    private Clock clock;
    private DoubleSupplier jitter;
    private Duration streamIdleTimeout;
    private Evaluation evaluation;
    private SSLContext sslContext;

    private Builder() {}

    /**
     * The server's base URL. Required. Trailing slashes are removed.
     *
     * @param value the URL
     * @return this builder
     */
    public Builder url(String value) {
      this.url = value;
      return this;
    }

    /**
     * The server SDK key. Required.
     *
     * @param value the key
     * @return this builder
     */
    public Builder sdkKey(String value) {
      this.sdkKey = value;
      return this;
    }

    /**
     * Whether to follow the live update stream. Default {@code true}; {@code false} polls only.
     *
     * @param value whether to stream
     * @return this builder
     */
    public Builder stream(boolean value) {
      this.stream = value;
      return this;
    }

    /**
     * How often to poll when the stream is down or off, and the longest reconnect wait. Default 30
     * seconds.
     *
     * @param value the interval; zero or negative means the default
     * @return this builder
     */
    public Builder pollInterval(Duration value) {
      this.pollInterval = value;
      return this;
    }

    /**
     * How often to send usage counts. Default 30 seconds.
     *
     * @param value the interval; zero or negative means the default
     * @return this builder
     */
    public Builder flushInterval(Duration value) {
      this.flushInterval = value;
      return this;
    }

    /**
     * The bound on a request, reading its body included. Default 10 seconds.
     *
     * @param value the timeout; zero or negative means the default
     * @return this builder
     */
    public Builder requestTimeout(Duration value) {
      this.requestTimeout = value;
      return this;
    }

    /**
     * A hook called after every evaluation, on the caller's thread. Default none.
     *
     * @param value the hook
     * @return this builder
     */
    public Builder afterEvaluation(Consumer<EvaluationEvent> value) {
      this.afterEvaluation = value;
      return this;
    }

    /**
     * Where failures the SDK absorbs go. Default: ignored. It may be called on the SDK's own
     * threads.
     *
     * @param value the callback
     * @return this builder
     */
    public Builder onError(Consumer<Throwable> value) {
      this.onError = value;
      return this;
    }

    /** Test seam: the clock for {@code lastSyncAt} and the UTC day of a count. */
    Builder clock(Clock value) {
      this.clock = value;
      return this;
    }

    /** Test seam: the jitter source, a number in [0, 1). */
    Builder jitter(DoubleSupplier value) {
      this.jitter = value;
      return this;
    }

    /** Test seam: how long a stream may go without a byte (default 60 seconds). */
    Builder streamIdleTimeout(Duration value) {
      this.streamIdleTimeout = value;
      return this;
    }

    /** Test seam: replaces the evaluator. */
    Builder evaluation(Evaluation value) {
      this.evaluation = value;
      return this;
    }

    /** Test seam: the TLS configuration of the HTTP client. */
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
