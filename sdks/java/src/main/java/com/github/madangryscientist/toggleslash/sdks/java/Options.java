package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Evaluator;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.time.Clock;
import java.time.Duration;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;
import javax.net.ssl.SSLContext;

/**
 * What {@link Client#create(Options)} reads. A field left unset takes its default, and a duration
 * that is zero or negative takes it too. The object is read once, at {@code create}.
 */
public final class Options {
  /** Evaluation as the client calls it; a seam for the package's own tests. */
  @FunctionalInterface
  interface Evaluation {
    Result evaluate(Ruleset ruleset, String flagKey, Json context);
  }

  String url;
  String sdkKey;
  Boolean stream;
  Duration pollInterval;
  Duration flushInterval;
  Duration requestTimeout;
  Consumer<EvaluationEvent> afterEvaluation;
  Consumer<Throwable> onError;

  // Test seams, set only by this package's tests.
  Duration streamIdle;
  Clock clock;
  DoubleSupplier random;
  Evaluation evaluation = Evaluator::evaluate;
  SSLContext sslContext;

  /** Creates options with nothing set. */
  public Options() {}

  /**
   * The server's base URL, such as {@code https://flags.example.com}. Required. Trailing slashes
   * are removed.
   *
   * @param value the URL
   * @return these options
   */
  public Options url(String value) {
    this.url = value;
    return this;
  }

  /**
   * A server SDK key. Required.
   *
   * @param value the key
   * @return these options
   */
  public Options sdkKey(String value) {
    this.sdkKey = value;
    return this;
  }

  /**
   * Whether to follow the live stream. Default true; false polls only.
   *
   * @param value on or off
   * @return these options
   */
  public Options stream(boolean value) {
    this.stream = value;
    return this;
  }

  /**
   * How often to poll when the stream is down or off, and the longest reconnect wait. Default 30 s.
   *
   * @param value the interval
   * @return these options
   */
  public Options pollInterval(Duration value) {
    this.pollInterval = value;
    return this;
  }

  /**
   * How often usage counts are sent. Default 30 s.
   *
   * @param value the interval
   * @return these options
   */
  public Options flushInterval(Duration value) {
    this.flushInterval = value;
    return this;
  }

  /**
   * The longest any one request may take, body included, and so the longest {@code ready} and
   * {@code close} wait. Default 10 s.
   *
   * @param value the timeout
   * @return these options
   */
  public Options requestTimeout(Duration value) {
    this.requestTimeout = value;
    return this;
  }

  /**
   * Called synchronously on the caller's thread after every {@code variation} and {@code
   * variationDetail}, a fallback included. What it throws goes to {@code onError}.
   *
   * @param value the hook
   * @return these options
   */
  public Options afterEvaluation(Consumer<EvaluationEvent> value) {
    this.afterEvaluation = value;
    return this;
  }

  /**
   * Told about every failure the SDK absorbs. It is never needed for correctness, and what it
   * throws is ignored.
   *
   * @param value the callback
   * @return these options
   */
  public Options onError(Consumer<Throwable> value) {
    this.onError = value;
    return this;
  }

  Options streamIdle(Duration value) {
    this.streamIdle = value;
    return this;
  }

  Options clock(Clock value) {
    this.clock = value;
    return this;
  }

  Options random(DoubleSupplier value) {
    this.random = value;
    return this;
  }

  Options evaluation(Evaluation value) {
    this.evaluation = value;
    return this;
  }

  Options sslContext(SSLContext value) {
    this.sslContext = value;
    return this;
  }
}
