package com.github.madangryscientist.toggleslash.sdks.java.internal;

import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.StreamState;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpHeaders;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.time.Duration;
import java.util.Set;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;
import java.util.function.UnaryOperator;

/**
 * The state the client, the sync loop and the usage flush share: the settings, the one atomic
 * {@link Core}, the stop signal, the requests in flight, the Retry-After holds and the ready latch.
 */
public final class Hub {
  /** A completed HTTP exchange whose body was read. */
  public record Reply(int status, byte[] body, HttpHeaders headers) {}

  /** A request that did not complete; {@code code} is null when {@code close} abandoned it. */
  public static final class Failure extends Exception {
    private static final long serialVersionUID = 1L;
    private final String code;

    Failure(String code) {
      super(code, null, false, false);
      this.code = code;
    }

    /**
     * The error code.
     *
     * @return {@code timeout} or {@code network}, or null when the request was abandoned
     */
    public String code() {
      return code;
    }
  }

  private static final Duration DEFAULT_POLL = Duration.ofSeconds(30);
  private static final Duration DEFAULT_FLUSH = Duration.ofSeconds(30);
  private static final Duration DEFAULT_TIMEOUT = Duration.ofSeconds(10);

  private final String base;
  private final String sdkKey;
  private final boolean streamOn;
  private final Duration pollInterval;
  private final Duration flushInterval;
  private final Duration requestTimeout;
  private final Options options;
  private final Tuning tuning;
  private final HttpClient http;
  private final Gate stop = new Gate();
  private final AtomicReference<Core> core;
  private final Set<Runnable> inflight = ConcurrentHashMap.newKeySet();
  private final AtomicBoolean closing = new AtomicBoolean();
  private final AtomicLong rulesHold;
  private final AtomicLong usageHold;
  private final CountDownLatch readyLatch = new CountDownLatch(1);
  private final AtomicBoolean readySettled = new AtomicBoolean();
  private volatile boolean readyValue;

  /**
   * Resolves the options.
   *
   * @param given the options, null for none
   * @param tuning test knobs, null for none
   */
  public Hub(Options given, Tuning tuning) {
    this.options = given == null ? Options.builder().build() : given;
    this.tuning = tuning == null ? new Tuning() : tuning;
    String url = options.url() == null ? "" : options.url();
    int end = url.length();
    while (end > 0 && url.charAt(end - 1) == '/') {
      end--;
    }
    this.base = url.substring(0, end);
    this.sdkKey = options.sdkKey() == null ? "" : options.sdkKey();
    this.streamOn = options.stream() == null || options.stream();
    this.pollInterval = positive(options.pollInterval(), DEFAULT_POLL);
    this.flushInterval = positive(options.flushInterval(), DEFAULT_FLUSH);
    this.requestTimeout = positive(options.requestTimeout(), DEFAULT_TIMEOUT);
    this.http =
        this.tuning.http != null
            ? this.tuning.http
            : HttpClient.newBuilder()
                .version(HttpClient.Version.HTTP_1_1)
                .followRedirects(HttpClient.Redirect.NEVER)
                .connectTimeout(requestTimeout)
                .build();
    this.core =
        new AtomicReference<>(
            new Core(null, null, null, streamOn ? StreamState.connecting : StreamState.off, null));
    long now = this.tuning.clock.monoNanos();
    this.rulesHold = new AtomicLong(now);
    this.usageHold = new AtomicLong(now);
  }

  private static Duration positive(Duration value, Duration fallback) {
    return value == null || value.isZero() || value.isNegative() ? fallback : value;
  }

  /**
   * The options as given.
   *
   * @return the options
   */
  public Options options() {
    return options;
  }

  /**
   * The server URL without trailing slashes.
   *
   * @return the base URL
   */
  public String base() {
    return base;
  }

  /**
   * Whether the stream is on.
   *
   * @return true when {@code stream} is on
   */
  public boolean streamOn() {
    return streamOn;
  }

  /**
   * The polling interval.
   *
   * @return the interval
   */
  public Duration pollInterval() {
    return pollInterval;
  }

  /**
   * The usage flush interval.
   *
   * @return the interval
   */
  public Duration flushInterval() {
    return flushInterval;
  }

  /**
   * The request timeout.
   *
   * @return the timeout
   */
  public Duration requestTimeout() {
    return requestTimeout;
  }

  /**
   * The test knobs.
   *
   * @return the tuning
   */
  public Tuning tuning() {
    return tuning;
  }

  /**
   * The stop signal.
   *
   * @return the gate, open once {@code close} began
   */
  public Gate stop() {
    return stop;
  }

  /**
   * The current snapshot.
   *
   * @return the core
   */
  public Core core() {
    return core.get();
  }

  /**
   * Whether {@code close} has begun.
   *
   * @return true after the first {@code close}
   */
  public boolean closing() {
    return closing.get();
  }

  /**
   * The time on the monotonic clock.
   *
   * @return nanoseconds
   */
  public long mono() {
    return tuning.clock.monoNanos();
  }

  /**
   * Wall time.
   *
   * @return epoch milliseconds
   */
  public long now() {
    return tuning.clock.nowMillis();
  }

  /**
   * Replaces the snapshot with a function of it; a closed client is left as it is.
   *
   * @param change the change
   */
  void update(UnaryOperator<Core> change) {
    core.updateAndGet(c -> c.stream() == StreamState.closed ? c : change.apply(c));
  }

  /**
   * Sets the stream state.
   *
   * @param state the state
   */
  void setStream(StreamState state) {
    update(c -> c.withStream(state));
  }

  /**
   * Starts closing: the state becomes closed, the stop gate opens and every request in flight is
   * abandoned. Only the first call does it.
   *
   * @return true for the call that started it
   */
  public boolean beginClose() {
    if (!closing.compareAndSet(false, true)) {
      return false;
    }
    core.updateAndGet(c -> c.withStream(StreamState.closed));
    stop.open();
    for (Runnable abort : inflight) {
      try {
        abort.run();
      } catch (RuntimeException e) {
        // an abort that fails changes nothing: the request ends on its own
      }
    }
    return true;
  }

  /**
   * Registers something that abandons a request, unless close already began.
   *
   * @param abort what to run on close
   * @return false when close already began (the caller abandons the request itself)
   */
  boolean track(Runnable abort) {
    inflight.add(abort);
    if (closing.get()) {
      inflight.remove(abort);
      return false;
    }
    return true;
  }

  void untrack(Runnable abort) {
    inflight.remove(abort);
  }

  /**
   * Hands an error to {@code onError}; an exception from the callback is ignored.
   *
   * @param error what happened
   */
  public void report(Throwable error) {
    Consumer<Throwable> callback = options.onError();
    if (callback == null) {
      return;
    }
    try {
      callback.accept(error);
    } catch (Throwable ignored) {
      // the callback's own failure is not ours to report
    }
  }

  /**
   * Lengthens the hold on the ruleset and stream routes.
   *
   * @param seconds how long
   */
  void holdRules(long seconds) {
    extend(rulesHold, seconds);
  }

  /**
   * Lengthens the hold on the events route.
   *
   * @param seconds how long
   */
  void holdUsage(long seconds) {
    extend(usageHold, seconds);
  }

  private void extend(AtomicLong hold, long seconds) {
    long until = mono() + TimeUnit.SECONDS.toNanos(seconds);
    hold.accumulateAndGet(until, (old, next) -> next - old > 0 ? next : old);
  }

  /**
   * How long the ruleset and stream routes are held.
   *
   * @return milliseconds, zero when not held
   */
  long rulesHoldMillis() {
    return remaining(rulesHold);
  }

  /**
   * How long the events route is held.
   *
   * @return milliseconds, zero when not held
   */
  public long usageHoldMillis() {
    return remaining(usageHold);
  }

  private long remaining(AtomicLong hold) {
    long nanos = hold.get() - mono();
    return nanos <= 0 ? 0 : (nanos + 999_999L) / 1_000_000L;
  }

  /**
   * Settles {@code ready} with whether a ruleset is held, if it is not settled yet.
   *
   * @param forceFalse settle false whatever is held
   */
  public void settleReady(boolean forceFalse) {
    if (readySettled.compareAndSet(false, true)) {
      readyValue = !forceFalse && core.get().ruleset() != null;
      readyLatch.countDown();
    }
  }

  /**
   * Waits for {@code ready} to settle, at most a grace past the request timeout.
   *
   * @return the settled answer
   */
  public boolean awaitReady() {
    try {
      long wait = requestTimeout.toMillis() + 1000;
      if (!readyLatch.await(wait, TimeUnit.MILLISECONDS)) {
        settleReady(false);
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return core.get().ruleset() != null;
    }
    return readyValue;
  }

  /**
   * What {@code ready} answers without waiting.
   *
   * @return the settled answer, or whether a ruleset is held when unsettled
   */
  public boolean readyNow() {
    return readySettled.get() && readyLatch.getCount() == 0
        ? readyValue
        : core.get().ruleset() != null;
  }

  /**
   * Builds a request to a path with the key.
   *
   * @param path the path
   * @return a builder with the URL and the authorization header
   * @throws IllegalArgumentException when the URL or key cannot form a request
   */
  HttpRequest.Builder request(String path) {
    return HttpRequest.newBuilder(URI.create(base + path))
        .header("Authorization", "Bearer " + sdkKey);
  }

  /**
   * Reads a Retry-After header: whole decimal seconds, spaces ignored, capped at 300.
   *
   * @param headers the response headers
   * @return the seconds, or -1 when the header is absent or not whole seconds
   */
  static long retryAfter(HttpHeaders headers) {
    String value = headers.firstValue("Retry-After").orElse(null);
    if (value == null) {
      return -1;
    }
    String trimmed = value.strip();
    if (trimmed.isEmpty()) {
      return -1;
    }
    long seconds = 0;
    for (int i = 0; i < trimmed.length(); i++) {
      char c = trimmed.charAt(i);
      if (c < '0' || c > '9') {
        return -1;
      }
      seconds = Math.min(300, seconds * 10 + (c - '0'));
    }
    return seconds;
  }

  HttpClient http() {
    return http;
  }

  /**
   * Sends a request and reads its whole body, bounded as a whole by the request timeout.
   *
   * @param request the request
   * @param abandonable whether {@code close} abandons it
   * @return the reply
   * @throws Failure when it timed out, failed or was abandoned
   */
  Reply send(HttpRequest request, boolean abandonable) throws Failure {
    CompletableFuture<HttpResponse<byte[]>> future;
    try {
      future = http.sendAsync(request, HttpResponse.BodyHandlers.ofByteArray());
    } catch (RuntimeException e) {
      throw new Failure("network");
    }
    Runnable abort = () -> future.cancel(true);
    if (abandonable && !track(abort)) {
      future.cancel(true);
      throw new Failure(null);
    }
    try {
      HttpResponse<byte[]> response = future.get(requestTimeout.toNanos(), TimeUnit.NANOSECONDS);
      return new Reply(response.statusCode(), response.body(), response.headers());
    } catch (TimeoutException e) {
      future.cancel(true);
      throw new Failure(abandonable && closing.get() ? null : "timeout");
    } catch (ExecutionException e) {
      throw new Failure(abandonable && closing.get() ? null : codeOf(e.getCause()));
    } catch (CancellationException e) {
      throw new Failure(abandonable && closing.get() ? null : "network");
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      future.cancel(true);
      throw new Failure(null);
    } finally {
      untrack(abort);
    }
  }

  static String codeOf(Throwable cause) {
    Throwable t = cause;
    while (t != null) {
      if (t instanceof HttpTimeoutException) {
        return "timeout";
      }
      t = t.getCause() == t ? null : t.getCause();
    }
    return "network";
  }
}
