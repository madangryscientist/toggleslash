package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Duration;
import java.util.function.Consumer;

/** The options of {@link Client#create(Options)}; build one with {@link #builder()}. */
public final class Options {
  private final String url;
  private final String sdkKey;
  private final Boolean stream;
  private final Duration pollInterval;
  private final Duration flushInterval;
  private final Duration requestTimeout;
  private final Hook afterEvaluation;
  private final Consumer<Throwable> onError;

  private Options(Builder b) {
    this.url = b.url;
    this.sdkKey = b.sdkKey;
    this.stream = b.stream;
    this.pollInterval = b.pollInterval;
    this.flushInterval = b.flushInterval;
    this.requestTimeout = b.requestTimeout;
    this.afterEvaluation = b.afterEvaluation;
    this.onError = b.onError;
  }

  /**
   * Starts a builder.
   *
   * @return a builder with nothing set
   */
  public static Builder builder() {
    return new Builder();
  }

  /**
   * The server URL.
   *
   * @return the URL as given, or null
   */
  public String url() {
    return url;
  }

  /**
   * The SDK key.
   *
   * @return the key, or null
   */
  public String sdkKey() {
    return sdkKey;
  }

  /**
   * Whether the stream was asked for.
   *
   * @return true or false when set, null when not given (which means on)
   */
  public Boolean stream() {
    return stream;
  }

  /**
   * The polling interval.
   *
   * @return the duration, or null when not given
   */
  public Duration pollInterval() {
    return pollInterval;
  }

  /**
   * The usage flush interval.
   *
   * @return the duration, or null when not given
   */
  public Duration flushInterval() {
    return flushInterval;
  }

  /**
   * The timeout of one request, body included.
   *
   * @return the duration, or null when not given
   */
  public Duration requestTimeout() {
    return requestTimeout;
  }

  /**
   * The hook run after every evaluation.
   *
   * @return the hook, or null
   */
  public Hook afterEvaluation() {
    return afterEvaluation;
  }

  /**
   * The error callback.
   *
   * @return the callback, or null
   */
  public Consumer<Throwable> onError() {
    return onError;
  }

  /** Builds {@link Options}. Nothing is checked here: a bad value is an error at run time. */
  public static final class Builder {
    private String url;
    private String sdkKey;
    private Boolean stream;
    private Duration pollInterval;
    private Duration flushInterval;
    private Duration requestTimeout;
    private Hook afterEvaluation;
    private Consumer<Throwable> onError;

    private Builder() {}

    /**
     * Sets the server URL, for example {@code https://flags.example.com}. A trailing slash is
     * removed.
     *
     * @param value the URL
     * @return this builder
     */
    public Builder url(String value) {
      this.url = value;
      return this;
    }

    /**
     * Sets the SDK key.
     *
     * @param value the key
     * @return this builder
     */
    public Builder sdkKey(String value) {
      this.sdkKey = value;
      return this;
    }

    /**
     * Turns the live stream on (the default) or off.
     *
     * @param value whether to follow the stream
     * @return this builder
     */
    public Builder stream(boolean value) {
      this.stream = value;
      return this;
    }

    /**
     * Sets the polling interval (default 30 seconds). Zero or negative means the default.
     *
     * @param value the interval
     * @return this builder
     */
    public Builder pollInterval(Duration value) {
      this.pollInterval = value;
      return this;
    }

    /**
     * Sets the usage flush interval (default 30 seconds). Zero or negative means the default.
     *
     * @param value the interval
     * @return this builder
     */
    public Builder flushInterval(Duration value) {
      this.flushInterval = value;
      return this;
    }

    /**
     * Sets the timeout of one request, body included (default 10 seconds). Zero or negative means
     * the default.
     *
     * @param value the timeout
     * @return this builder
     */
    public Builder requestTimeout(Duration value) {
      this.requestTimeout = value;
      return this;
    }

    /**
     * Sets the hook that runs after every evaluation, on the caller's thread.
     *
     * @param value the hook
     * @return this builder
     */
    public Builder afterEvaluation(Hook value) {
      this.afterEvaluation = value;
      return this;
    }

    /**
     * Sets the callback for errors the SDK absorbs. It runs on the thread that saw the failure.
     *
     * @param value the callback
     * @return this builder
     */
    public Builder onError(Consumer<Throwable> value) {
      this.onError = value;
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
