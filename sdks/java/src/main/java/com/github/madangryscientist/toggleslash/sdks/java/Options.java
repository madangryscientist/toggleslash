package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Duration;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;

/** What a client is built from. Make one with {@link #builder()}. */
public final class Options {
  static final Duration DEFAULT_INTERVAL = Duration.ofSeconds(30);
  static final Duration DEFAULT_TIMEOUT = Duration.ofSeconds(10);
  static final Duration DEFAULT_IDLE = Duration.ofSeconds(60);

  final String url;
  final String sdkKey;
  final boolean stream;
  final Duration pollInterval;
  final Duration flushInterval;
  final Duration requestTimeout;
  final Duration streamIdleTimeout;
  final AfterEvaluation afterEvaluation;
  final Consumer<Throwable> onError;
  final Clock clock;
  final DoubleSupplier random;

  private Options(Builder b) {
    String base = b.url == null ? "" : b.url;
    int end = base.length();
    while (end > 0 && base.charAt(end - 1) == '/') {
      end--;
    }
    this.url = base.substring(0, end);
    this.sdkKey = b.sdkKey == null ? "" : b.sdkKey;
    this.stream = b.stream == null || b.stream;
    this.pollInterval = positive(b.pollInterval, DEFAULT_INTERVAL);
    this.flushInterval = positive(b.flushInterval, DEFAULT_INTERVAL);
    this.requestTimeout = positive(b.requestTimeout, DEFAULT_TIMEOUT);
    this.streamIdleTimeout = positive(b.streamIdleTimeout, DEFAULT_IDLE);
    this.afterEvaluation = b.afterEvaluation;
    this.onError = b.onError;
    this.clock = b.clock == null ? Clock.SYSTEM : b.clock;
    this.random = b.random == null ? Math::random : b.random;
  }

  /** A null, zero or negative duration is not given: the default stands. */
  private static Duration positive(Duration given, Duration fallback) {
    if (given == null || given.isZero() || given.isNegative()) {
      return fallback;
    }
    return given;
  }

  /** A new builder with every option unset. */
  public static Builder builder() {
    return new Builder();
  }

  /** Collects options. Every setter returns the builder. */
  public static final class Builder {
    private String url;
    private String sdkKey;
    private Boolean stream;
    private Duration pollInterval;
    private Duration flushInterval;
    private Duration requestTimeout;
    private Duration streamIdleTimeout;
    private AfterEvaluation afterEvaluation;
    private Consumer<Throwable> onError;
    private Clock clock;
    private DoubleSupplier random;

    private Builder() {}

    /**
     * The server's base URL, e.g. {@code https://flags.example.com}. Required; trailing slashes go.
     */
    public Builder url(String value) {
      this.url = value;
      return this;
    }

    /** A server SDK key ({@code fs_server_...}). Required. A client key cannot read the ruleset. */
    public Builder sdkKey(String value) {
      this.sdkKey = value;
      return this;
    }

    /** Follow the stream (the default). False polls only. */
    public Builder stream(boolean value) {
      this.stream = value;
      return this;
    }

    /**
     * How often to poll when the stream is down or off, and the longest reconnect wait (default 30
     * s).
     */
    public Builder pollInterval(Duration value) {
      this.pollInterval = value;
      return this;
    }

    /** How often usage counts are sent (default 30 s). */
    public Builder flushInterval(Duration value) {
      this.flushInterval = value;
      return this;
    }

    /** The longest any one request may take, body included (default 10 s). */
    public Builder requestTimeout(Duration value) {
      this.requestTimeout = value;
      return this;
    }

    /** Called after every evaluation, on the caller's thread; see {@link AfterEvaluation}. */
    public Builder afterEvaluation(AfterEvaluation value) {
      this.afterEvaluation = value;
      return this;
    }

    /**
     * Told about every failure the SDK absorbs, on the thread that hit it. An exception it throws
     * is ignored.
     */
    public Builder onError(Consumer<Throwable> value) {
      this.onError = value;
      return this;
    }

    /** For tests: how long the stream may go without a byte before it is dropped (default 60 s). */
    public Builder streamIdleTimeout(Duration value) {
      this.streamIdleTimeout = value;
      return this;
    }

    /** For tests: the time and the waits every timer goes through. */
    public Builder clock(Clock value) {
      this.clock = value;
      return this;
    }

    /** For tests: the source of the backoff jitter, in [0, 1). */
    public Builder random(DoubleSupplier value) {
      this.random = value;
      return this;
    }

    /** Builds the options. */
    public Options build() {
      return new Options(this);
    }
  }
}
