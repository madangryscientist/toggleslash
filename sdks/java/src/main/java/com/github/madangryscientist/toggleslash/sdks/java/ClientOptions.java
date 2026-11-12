package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Duration;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;
import javax.net.ssl.SSLContext;

/**
 * The options of {@link ToggleSlashClient#create}. Build them with {@link #builder}. A setter that
 * is never called leaves the default, and an explicit {@code stream(false)} is a different value. A
 * {@link Duration} that is null, zero or negative falls back to the default.
 */
public final class ClientOptions {

  static final Duration DEFAULT_POLL_INTERVAL = Duration.ofSeconds(30);
  static final Duration DEFAULT_FLUSH_INTERVAL = Duration.ofSeconds(30);
  static final Duration DEFAULT_REQUEST_TIMEOUT = Duration.ofSeconds(10);
  static final Duration DEFAULT_WATCHDOG = Duration.ofSeconds(60);

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
  private final SSLContext sslContext;

  private ClientOptions(Builder b) {
    this.url = b.url == null ? "" : b.url;
    this.sdkKey = b.sdkKey == null ? "" : b.sdkKey;
    this.stream = b.stream;
    this.pollInterval = positive(b.pollInterval, DEFAULT_POLL_INTERVAL);
    this.flushInterval = positive(b.flushInterval, DEFAULT_FLUSH_INTERVAL);
    this.requestTimeout = positive(b.requestTimeout, DEFAULT_REQUEST_TIMEOUT);
    this.afterEvaluation = b.afterEvaluation;
    this.onError = b.onError;
    this.clock = b.clock;
    this.jitter = b.jitter;
    this.watchdog = positive(b.watchdog, DEFAULT_WATCHDOG);
    this.sslContext = b.sslContext;
  }

  private static Duration positive(Duration value, Duration fallback) {
    return value == null || value.isZero() || value.isNegative() ? fallback : value;
  }

  /**
   * Starts the options.
   *
   * @param url the server's base URL; trailing {@code /} characters are removed
   * @param sdkKey the server SDK key
   */
  public static Builder builder(String url, String sdkKey) {
    return new Builder(url, sdkKey);
  }

  String url() {
    int end = url.length();
    while (end > 0 && url.charAt(end - 1) == '/') {
      end--;
    }
    return url.substring(0, end);
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

  SSLContext sslContext() {
    return sslContext;
  }

  /** Builds {@link ClientOptions}. */
  public static final class Builder {

    private final String url;
    private final String sdkKey;
    private boolean stream = true;
    private Duration pollInterval = DEFAULT_POLL_INTERVAL;
    private Duration flushInterval = DEFAULT_FLUSH_INTERVAL;
    private Duration requestTimeout = DEFAULT_REQUEST_TIMEOUT;
    private Consumer<EvaluationEvent> afterEvaluation;
    private Consumer<Throwable> onError;
    private Clock clock = Clock.SYSTEM;
    private DoubleSupplier jitter =
        () -> 0.5 + 0.5 * java.util.concurrent.ThreadLocalRandom.current().nextDouble();
    private Duration watchdog = DEFAULT_WATCHDOG;
    private SSLContext sslContext;

    private Builder(String url, String sdkKey) {
      this.url = url;
      this.sdkKey = sdkKey;
    }

    /** Whether to follow the server's stream (default on); off means polling only. */
    public Builder stream(boolean value) {
      this.stream = value;
      return this;
    }

    /** The poll interval, and the ceiling of the stream's backoff (default 30 s). */
    public Builder pollInterval(Duration value) {
      this.pollInterval = value;
      return this;
    }

    /** How often pending usage counts are sent (default 30 s). */
    public Builder flushInterval(Duration value) {
      this.flushInterval = value;
      return this;
    }

    /** The bound of one request, its body included (default 10 s). */
    public Builder requestTimeout(Duration value) {
      this.requestTimeout = value;
      return this;
    }

    /**
     * A function called after every evaluation, a fallback included, on the caller's thread before
     * the evaluation returns. Anything it throws goes to {@code onError}.
     */
    public Builder afterEvaluation(Consumer<EvaluationEvent> value) {
      this.afterEvaluation = value;
      return this;
    }

    /**
     * A function that receives every failure the client absorbs (default: ignore). It is called on
     * the SDK's own threads for sync and usage failures, and on the caller's thread for an
     * evaluation or hook that raised. Anything it throws is ignored.
     */
    public Builder onError(Consumer<Throwable> value) {
      this.onError = value;
      return this;
    }

    /** Test seam: the time source. */
    Builder clock(Clock value) {
      this.clock = value;
      return this;
    }

    /** Test seam: the jitter multiplier of the stream backoff, in [0.5, 1). */
    Builder jitter(DoubleSupplier value) {
      this.jitter = value;
      return this;
    }

    /** Test seam: the length of the stream's quiet-time watchdog (default 60 s). */
    Builder watchdog(Duration value) {
      this.watchdog = value;
      return this;
    }

    /** Test seam: the TLS context, for a local certificate. */
    Builder sslContext(SSLContext value) {
      this.sslContext = value;
      return this;
    }

    /** The options. */
    public ClientOptions build() {
      return new ClientOptions(this);
    }
  }
}
