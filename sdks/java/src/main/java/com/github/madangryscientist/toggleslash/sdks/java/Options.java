package com.github.madangryscientist.toggleslash.sdks.java;

import java.net.http.HttpClient;
import java.time.Duration;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;
import javax.net.ssl.SSLContext;

/**
 * What {@link Client#create} takes. Build one with {@link #builder(String, String)}. An option that
 * is not given keeps its default; a given {@code false} or zero is used as it is.
 */
public final class Options {
  /** The default for {@code pollInterval} and {@code flushInterval}. */
  public static final Duration DEFAULT_INTERVAL = Duration.ofSeconds(30);

  /** The default for {@code requestTimeout}. */
  public static final Duration DEFAULT_REQUEST_TIMEOUT = Duration.ofSeconds(10);

  /** The longest a {@code Retry-After} holds a route. */
  static final Duration MAX_RETRY_AFTER = Duration.ofSeconds(300);

  /** How long a stream may go without a byte before it is dropped. */
  static final Duration STREAM_IDLE = Duration.ofSeconds(60);

  final String url;
  final String sdkKey;
  final Boolean stream;
  final Duration pollInterval;
  final Duration flushInterval;
  final Duration requestTimeout;
  final Consumer<EvaluationEvent> afterEvaluation;
  final Consumer<Throwable> onError;

  // Test seams. They are options on create, package-private, never a global.
  final HttpClient httpClient;
  final SSLContext sslContext;
  final Clock clock;
  final DoubleSupplier jitter;
  final Duration streamIdle;
  final Duration retryAfterCap;
  final Evaluate evaluator;

  private Options(Builder b) {
    this.url = b.url;
    this.sdkKey = b.sdkKey;
    this.stream = b.stream;
    this.pollInterval = b.pollInterval;
    this.flushInterval = b.flushInterval;
    this.requestTimeout = b.requestTimeout;
    this.afterEvaluation = b.afterEvaluation;
    this.onError = b.onError;
    this.httpClient = b.httpClient;
    this.sslContext = b.sslContext;
    this.clock = b.clock;
    this.jitter = b.jitter;
    this.streamIdle = b.streamIdle;
    this.retryAfterCap = b.retryAfterCap;
    this.evaluator = b.evaluator;
  }

  /**
   * Starts building options.
   *
   * @param url the server's base URL; trailing slashes are removed
   * @param sdkKey the server SDK key
   * @return a builder
   */
  public static Builder builder(String url, String sdkKey) {
    return new Builder(url, sdkKey);
  }

  /** Builds {@link Options}. */
  public static final class Builder {
    private final String url;
    private final String sdkKey;
    private Boolean stream;
    private Duration pollInterval;
    private Duration flushInterval;
    private Duration requestTimeout;
    private Consumer<EvaluationEvent> afterEvaluation;
    private Consumer<Throwable> onError;
    private HttpClient httpClient;
    private SSLContext sslContext;
    private Clock clock;
    private DoubleSupplier jitter;
    private Duration streamIdle;
    private Duration retryAfterCap;
    private Evaluate evaluator;

    private Builder(String url, String sdkKey) {
      this.url = url;
      this.sdkKey = sdkKey;
    }

    /**
     * Follows {@code GET /sdk/v1/stream} (the default) or, when false, only polls.
     *
     * @param value whether to follow the stream
     * @return this
     */
    public Builder stream(boolean value) {
      this.stream = value;
      return this;
    }

    /**
     * How often to poll, and the longest backoff between stream attempts (default 30 s).
     *
     * @param value the interval
     * @return this
     */
    public Builder pollInterval(Duration value) {
      this.pollInterval = value;
      return this;
    }

    /**
     * How often usage counts are sent (default 30 s).
     *
     * @param value the interval
     * @return this
     */
    public Builder flushInterval(Duration value) {
      this.flushInterval = value;
      return this;
    }

    /**
     * The longest any one request may take, body included, and so about the longest {@code ready}
     * and {@code close} wait (default 10 s).
     *
     * @param value the limit
     * @return this
     */
    public Builder requestTimeout(Duration value) {
      this.requestTimeout = value;
      return this;
    }

    /**
     * Runs after every evaluation, on the calling thread. A throw goes to {@code onError}.
     *
     * @param value the hook
     * @return this
     */
    public Builder afterEvaluation(Consumer<EvaluationEvent> value) {
      this.afterEvaluation = value;
      return this;
    }

    /**
     * Receives every failure the client absorbs. A throw from it is ignored.
     *
     * @param value the callback
     * @return this
     */
    public Builder onError(Consumer<Throwable> value) {
      this.onError = value;
      return this;
    }

    Builder httpClient(HttpClient value) {
      this.httpClient = value;
      return this;
    }

    Builder sslContext(SSLContext value) {
      this.sslContext = value;
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

    Builder streamIdle(Duration value) {
      this.streamIdle = value;
      return this;
    }

    Builder retryAfterCap(Duration value) {
      this.retryAfterCap = value;
      return this;
    }

    Builder evaluator(Evaluate value) {
      this.evaluator = value;
      return this;
    }

    /**
     * Finishes the options.
     *
     * @return the options
     */
    public Options build() {
      return new Options(this);
    }
  }
}
