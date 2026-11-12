package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Evaluator;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.net.http.HttpClient;
import java.time.Duration;
import java.util.concurrent.ThreadLocalRandom;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;

/** What {@link Client#create} takes. Build it with {@link #builder}. */
public final class Options {
  static final Duration DEFAULT_POLL = Duration.ofSeconds(30);
  static final Duration DEFAULT_FLUSH = Duration.ofSeconds(30);
  static final Duration DEFAULT_TIMEOUT = Duration.ofSeconds(10);
  static final Duration DEFAULT_WATCHDOG = Duration.ofSeconds(60);

  /** Evaluates; the seam lets a test make evaluation raise. */
  @FunctionalInterface
  interface EvaluatorFn {
    Result evaluate(Ruleset ruleset, String flagKey, Object context);
  }

  final String url;
  final String sdkKey;
  final boolean stream;
  final Duration pollInterval;
  final Duration flushInterval;
  final Duration requestTimeout;
  final AfterEvaluation afterEvaluation;
  final Consumer<Throwable> onError;
  final Clock clock;
  final DoubleSupplier jitter;
  final Duration watchdog;
  final HttpClient httpClient;
  final EvaluatorFn evaluator;

  private Options(Builder b) {
    this.url = stripSlashes(b.url == null ? "" : b.url);
    this.sdkKey = b.sdkKey == null ? "" : b.sdkKey;
    this.stream = b.stream == null || b.stream;
    this.pollInterval = positive(b.pollInterval, DEFAULT_POLL);
    this.flushInterval = positive(b.flushInterval, DEFAULT_FLUSH);
    this.requestTimeout = positive(b.requestTimeout, DEFAULT_TIMEOUT);
    this.afterEvaluation = b.afterEvaluation;
    this.onError = b.onError;
    this.clock = b.clock == null ? Clock.SYSTEM : b.clock;
    this.jitter =
        b.jitter == null ? () -> 0.5 + 0.5 * ThreadLocalRandom.current().nextDouble() : b.jitter;
    this.watchdog = positive(b.watchdog, DEFAULT_WATCHDOG);
    this.httpClient = b.httpClient;
    this.evaluator = b.evaluator == null ? Evaluator::evaluate : b.evaluator;
  }

  private static Duration positive(Duration value, Duration fallback) {
    return value == null || value.isZero() || value.isNegative() ? fallback : value;
  }

  private static String stripSlashes(String url) {
    int end = url.length();
    while (end > 0 && url.charAt(end - 1) == '/') {
      end--;
    }
    return url.substring(0, end);
  }

  /**
   * Starts building options.
   *
   * @param url the server's base URL
   * @param sdkKey the server SDK key
   * @return a builder
   */
  public static Builder builder(String url, String sdkKey) {
    return new Builder(url, sdkKey);
  }

  /** Builds {@link Options}. A value not given, or a duration of zero or less, is the default. */
  public static final class Builder {
    private final String url;
    private final String sdkKey;
    private Boolean stream;
    private Duration pollInterval;
    private Duration flushInterval;
    private Duration requestTimeout;
    private AfterEvaluation afterEvaluation;
    private Consumer<Throwable> onError;
    private Clock clock;
    private DoubleSupplier jitter;
    private Duration watchdog;
    private HttpClient httpClient;
    private EvaluatorFn evaluator;

    private Builder(String url, String sdkKey) {
      this.url = url;
      this.sdkKey = sdkKey;
    }

    /**
     * Follows the stream (default true); false polls only.
     *
     * @param value on or off
     * @return this
     */
    public Builder stream(boolean value) {
      this.stream = value;
      return this;
    }

    /**
     * The wait between polls, and the ceiling of the stream backoff (default 30 s).
     *
     * @param value the interval
     * @return this
     */
    public Builder pollInterval(Duration value) {
      this.pollInterval = value;
      return this;
    }

    /**
     * The wait between usage flushes (default 30 s).
     *
     * @param value the interval
     * @return this
     */
    public Builder flushInterval(Duration value) {
      this.flushInterval = value;
      return this;
    }

    /**
     * The bound of one request, the body included (default 10 s).
     *
     * @param value the bound
     * @return this
     */
    public Builder requestTimeout(Duration value) {
      this.requestTimeout = value;
      return this;
    }

    /**
     * The hook called after every evaluation, on the caller's thread.
     *
     * @param value the hook
     * @return this
     */
    public Builder afterEvaluation(AfterEvaluation value) {
      this.afterEvaluation = value;
      return this;
    }

    /**
     * Receives every failure the SDK absorbs. It may be called on an SDK thread or the caller's;
     * what it throws is ignored.
     *
     * @param value the callback
     * @return this
     */
    public Builder onError(Consumer<Throwable> value) {
      this.onError = value;
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

    Builder watchdog(Duration value) {
      this.watchdog = value;
      return this;
    }

    Builder httpClient(HttpClient value) {
      this.httpClient = value;
      return this;
    }

    Builder evaluator(EvaluatorFn value) {
      this.evaluator = value;
      return this;
    }

    /**
     * Builds the options. It does not fail.
     *
     * @return the options
     */
    public Options build() {
      return new Options(this);
    }
  }
}
