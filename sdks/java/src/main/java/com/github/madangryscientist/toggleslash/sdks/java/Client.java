package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Evaluator;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.time.Duration;
import java.time.Instant;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ThreadLocalRandom;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;

/**
 * The ToggleSlash server SDK for Java. It keeps a ruleset current from the server and evaluates
 * flags locally against it, so {@code variation} never waits on the network. Nothing here throws at
 * a caller: a failure becomes the caller's fallback with an {@code ERROR} reason, or goes to {@code
 * onError}, or both. Every method is safe to call from many threads at once.
 *
 * <p>A context is a JSON value in Java types: a {@link Map} with {@code String} keys, a {@link
 * List}, a {@link String}, a {@link Boolean}, a {@link Number} (read as a binary64 value) or null.
 */
public final class Client implements AutoCloseable {
  private enum Kind {
    BOOL,
    STRING,
    NUMBER,
    LONG,
    INTEGER,
    JSON,
    SAME_KIND
  }

  /** Marks an answer whose value does not fit the accessor. */
  private static final Object MISMATCH = new Object();

  private final Reporter reporter;
  private final Consumer<EvaluationEvent> afterEvaluation;
  private final Evaluate evaluator;
  private final Stop stop = new Stop();
  private final Http http;
  private final Sync sync;
  private final Usage usage;
  private final long timeoutMs;
  private final CompletableFuture<Boolean> firstLoad = new CompletableFuture<>();
  private final AtomicBoolean closing = new AtomicBoolean();
  private final AtomicReference<CompletableFuture<Void>> shutdown = new AtomicReference<>();
  private final Thread syncThread;
  private final Thread flushThread;

  private Client(Options options) {
    Options given = options;
    String url = given.url == null ? "" : given.url;
    int end = url.length();
    while (end > 0 && url.charAt(end - 1) == '/') {
      end--;
    }
    String base = url.substring(0, end);
    String key = given.sdkKey == null ? "" : given.sdkKey;
    this.reporter = new Reporter(given.onError);
    this.afterEvaluation = given.afterEvaluation;
    this.evaluator = given.evaluator != null ? given.evaluator : Evaluator::evaluate;
    this.timeoutMs = Http.millis(positive(given.requestTimeout, Options.DEFAULT_REQUEST_TIMEOUT));
    long pollMs = Http.millis(positive(given.pollInterval, Options.DEFAULT_INTERVAL));
    long flushMs = Http.millis(positive(given.flushInterval, Options.DEFAULT_INTERVAL));
    long idleMs = Http.millis(positive(given.streamIdle, Options.STREAM_IDLE));
    long capMs = Http.millis(positive(given.retryAfterCap, Options.MAX_RETRY_AFTER));
    Clock clock = given.clock != null ? given.clock : Clock.SYSTEM;
    this.http = new Http(given.httpClient, given.sslContext);
    this.usage = new Usage(base, key, http, clock, reporter, timeoutMs, capMs);
    this.sync =
        new Sync(
            base,
            key,
            http,
            clock,
            given.jitter != null ? given.jitter : () -> ThreadLocalRandom.current().nextDouble(),
            reporter,
            stop,
            given.stream == null || given.stream,
            pollMs,
            timeoutMs,
            idleMs,
            capMs);
    this.syncThread =
        new SdkThread(
            this,
            "toggleslash-sync",
            () -> {
              try {
                sync.run(firstLoad::complete);
              } catch (Throwable failure) {
                reporter.report(failure);
              } finally {
                firstLoad.complete(false);
              }
            });
    this.flushThread =
        new SdkThread(
            this,
            "toggleslash-flush",
            () -> {
              try {
                while (!stop.isSignalled()) {
                  if (!clock.sleep(flushMs, stop)) {
                    break;
                  }
                  usage.flush();
                }
              } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
              } catch (Throwable failure) {
                reporter.report(failure);
              }
            });
  }

  /**
   * Creates a client and starts its first load. It does not wait for the load and never throws: a
   * URL it cannot use gives a {@code network} error on {@link #status}.
   *
   * @param options the options, from {@link Options#builder}
   * @return the client
   */
  public static Client create(Options options) {
    Client client = new Client(options == null ? Options.builder("", "").build() : options);
    client.start();
    return client;
  }

  /** The given duration, or the default when it is null, zero or negative (contract section 2). */
  private static Duration positive(Duration given, Duration fallback) {
    return given == null || given.isZero() || given.isNegative() ? fallback : given;
  }

  private void start() {
    syncThread.start();
    flushThread.start();
  }

  /**
   * Waits for the first load to finish, bounded by {@code requestTimeout}. The answer is fixed when
   * the first attempt settles: true if a ruleset had loaded, false if not (or after {@link #close}
   * if it had not settled). Called from one of the SDK's own callbacks it returns at once with the
   * state held.
   *
   * @return whether the first attempt loaded a ruleset
   */
  public boolean ready() {
    try {
      if (firstLoad.isDone()) {
        return firstLoad.getNow(false);
      }
      if (SdkThread.isOwnedBy(this)) {
        return sync.state.get().ruleset() != null;
      }
      long bound = timeoutMs > Long.MAX_VALUE / 4 ? Long.MAX_VALUE : 2 * timeoutMs + 1_000;
      return firstLoad.get(bound, TimeUnit.MILLISECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return false;
    } catch (ExecutionException | TimeoutException | RuntimeException e) {
      return false;
    }
  }

  /**
   * Evaluates a flag. The accessor follows the fallback's class: a {@link Boolean}, {@link String},
   * {@link Double}, {@link Integer} or {@link Long} takes a value of that kind (a whole number in
   * range for the last two), a {@link List} an array, a {@link Map} an object, and null takes any
   * JSON value.
   *
   * @param <T> the fallback's type
   * @param flagKey the flag
   * @param context the context
   * @param fallback what to answer when the flag cannot be served
   * @return the served value, or the fallback
   */
  public <T> T variation(String flagKey, Object context, T fallback) {
    return variationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #variation} with the variation index and the reason.
   *
   * @param <T> the fallback's type
   * @param flagKey the flag
   * @param context the context
   * @param fallback what to answer when the flag cannot be served
   * @return the answer
   */
  public <T> Detail<T> variationDetail(String flagKey, Object context, T fallback) {
    return run(flagKey, context, kindOf(fallback), fallback);
  }

  /**
   * A boolean flag.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the value or the fallback
   */
  public boolean boolVariation(String flagKey, Object context, boolean fallback) {
    return boolVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * A boolean flag with its reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the answer
   */
  public Detail<Boolean> boolVariationDetail(String flagKey, Object context, boolean fallback) {
    return run(flagKey, context, Kind.BOOL, fallback);
  }

  /**
   * A string flag. The fallback must not be null.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the value or the fallback
   */
  public String stringVariation(String flagKey, Object context, String fallback) {
    return stringVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * A string flag with its reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the answer
   */
  public Detail<String> stringVariationDetail(String flagKey, Object context, String fallback) {
    return run(flagKey, context, Kind.STRING, fallback);
  }

  /**
   * A number flag, as a binary64 value.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the value or the fallback
   */
  public double doubleVariation(String flagKey, Object context, double fallback) {
    return doubleVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * A number flag with its reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the answer
   */
  public Detail<Double> doubleVariationDetail(String flagKey, Object context, double fallback) {
    return run(flagKey, context, Kind.NUMBER, fallback);
  }

  /**
   * A whole-number flag as a 64-bit signed integer. A served number with a fraction, or outside
   * that range, is a {@code TYPE_MISMATCH} and the fallback.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the value or the fallback
   */
  public long longVariation(String flagKey, Object context, long fallback) {
    return longVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * A whole-number flag with its reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the answer
   */
  public Detail<Long> longVariationDetail(String flagKey, Object context, long fallback) {
    return run(flagKey, context, Kind.LONG, fallback);
  }

  /**
   * A flag of any JSON kind, in the SDK's JSON types ({@code null}, {@link Boolean}, {@link
   * Double}, {@link String}, {@link List}, {@link Map}). No kind is checked.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback, any JSON value
   * @return the value or the fallback
   */
  public Object jsonVariation(String flagKey, Object context, Object fallback) {
    return jsonVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * A flag of any JSON kind with its reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback, any JSON value
   * @return the answer
   */
  public Detail<Object> jsonVariationDetail(String flagKey, Object context, Object fallback) {
    return run(flagKey, context, Kind.JSON, fallback);
  }

  private static Kind kindOf(Object fallback) {
    if (fallback instanceof Boolean) {
      return Kind.BOOL;
    }
    if (fallback instanceof String) {
      return Kind.STRING;
    }
    if (fallback instanceof Integer) {
      return Kind.INTEGER;
    }
    if (fallback instanceof Long) {
      return Kind.LONG;
    }
    if (fallback instanceof Number) {
      return Kind.NUMBER;
    }
    return Kind.SAME_KIND;
  }

  private enum JsonKind {
    NULL,
    BOOL,
    NUMBER,
    STRING,
    ARRAY,
    OBJECT
  }

  private static JsonKind jsonKind(Object value) {
    if (value == null) {
      return JsonKind.NULL;
    }
    if (value instanceof Boolean) {
      return JsonKind.BOOL;
    }
    if (value instanceof Number) {
      return JsonKind.NUMBER;
    }
    if (value instanceof String) {
      return JsonKind.STRING;
    }
    return value instanceof List ? JsonKind.ARRAY : JsonKind.OBJECT;
  }

  /** A whole served number as a long, or {@link #MISMATCH}. */
  private static Object wholeNumber(Object served) {
    if (!(served instanceof Number)) {
      return MISMATCH;
    }
    double d = ((Number) served).doubleValue();
    // 2^63 is the first double past the signed 64-bit range; -2^63 is its first value.
    if (!(d >= -9.223372036854775808E18 && d < 9.223372036854775808E18) || d != Math.rint(d)) {
      return MISMATCH;
    }
    return (long) d;
  }

  private static Object convert(Kind kind, Object served, Object fallback) {
    switch (kind) {
      case BOOL:
        return served instanceof Boolean ? served : MISMATCH;
      case STRING:
        return served instanceof String ? served : MISMATCH;
      case NUMBER:
        return served instanceof Number ? (Object) ((Number) served).doubleValue() : MISMATCH;
      case LONG:
        return wholeNumber(served);
      case INTEGER:
        {
          Object whole = wholeNumber(served);
          if (whole == MISMATCH) {
            return MISMATCH;
          }
          long value = (Long) whole;
          return value >= Integer.MIN_VALUE && value <= Integer.MAX_VALUE
              ? (Object) (int) value
              : MISMATCH;
        }
      case SAME_KIND:
        return fallback == null || jsonKind(served) == jsonKind(fallback) ? served : MISMATCH;
      default:
        return served;
    }
  }

  private <T> Detail<T> fallbackDetail(T fallback, ErrorCode code) {
    return new Detail<>(fallback, null, Reason.error(code));
  }

  private <T> Detail<T> run(String flagKey, Object context, Kind kind, T fallback) {
    Snapshot held = sync.state.get();
    Detail<T> detail;
    try {
      detail = detail(held.ruleset(), flagKey, context, kind, fallback);
    } catch (Throwable failure) {
      reporter.report(failure);
      detail = fallbackDetail(fallback, ErrorCode.EVALUATION_FAILED);
    }
    if (afterEvaluation != null) {
      runHook(held, flagKey, context, detail);
    }
    return detail;
  }

  @SuppressWarnings("unchecked")
  private <T> Detail<T> detail(
      Ruleset ruleset, String flagKey, Object context, Kind kind, T fallback) {
    if (ruleset == null) {
      return fallbackDetail(fallback, ErrorCode.NOT_READY);
    }
    Result result = evaluator.evaluate(ruleset, flagKey, context);
    if (result.variationIndex() == null) {
      return new Detail<>(fallback, null, result.reason());
    }
    Object value = convert(kind, result.value(), fallback);
    if (value == MISMATCH) {
      return fallbackDetail(fallback, ErrorCode.TYPE_MISMATCH);
    }
    usage.record(flagKey, result.variationIndex());
    return new Detail<>((T) value, result.variationIndex(), result.reason());
  }

  private void runHook(Snapshot held, String flagKey, Object context, Detail<?> detail) {
    try {
      Long version = held.ruleset() == null ? null : held.ruleset().flagVersion(flagKey);
      afterEvaluation.accept(
          new EvaluationEvent(
              flagKey,
              context,
              detail.value(),
              detail.variationIndex(),
              detail.reason(),
              held.etag(),
              version));
    } catch (Throwable failure) {
      reporter.report(failure);
    }
  }

  /**
   * What the client has loaded and how its stream is doing: one consistent snapshot, local to the
   * process. It does no I/O and never waits.
   *
   * @return the status
   */
  public Status status() {
    Snapshot held = sync.state.get();
    return new Status(
        held.etag(),
        held.lastSyncAt() == null ? null : Instant.ofEpochMilli(held.lastSyncAt()),
        closing.get() ? StreamState.CLOSED : held.stream(),
        held.lastError());
  }

  /**
   * Stops following the server, sends the last usage counts and returns once that is done, within
   * about twice {@code requestTimeout}. It is idempotent, and every call returns after the same
   * shutdown. Called on one of the SDK's own threads (from a callback) it stops the rest and
   * returns without waiting for that thread's work. Evaluation keeps answering from the held
   * ruleset afterwards, and counts nothing.
   */
  @Override
  public void close() {
    CompletableFuture<Void> mine = new CompletableFuture<>();
    CompletableFuture<Void> existing = shutdown.compareAndExchange(null, mine);
    if (existing == null) {
      begin();
      if (SdkThread.isOwnedBy(this)) {
        Thread closer = new Thread(() -> finishShutdown(mine), "toggleslash-close");
        closer.setDaemon(true);
        closer.start();
        return;
      }
      finishShutdown(mine);
      return;
    }
    if (SdkThread.isOwnedBy(this)) {
      return;
    }
    try {
      existing.get(2 * timeoutMs + 5_000, TimeUnit.MILLISECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    } catch (ExecutionException | TimeoutException | RuntimeException e) {
      // Shutdown is best effort and bounded: there is nothing more to wait for.
    }
  }

  /** Step 1 and 2: stop counting, stop following, stop the flush loop, cancel what is in flight. */
  private void begin() {
    closing.set(true);
    usage.stopCounting();
    stop.signal();
    http.abortAll();
    firstLoad.complete(false);
  }

  private void finishShutdown(CompletableFuture<Void> done) {
    try {
      long bound = 2 * timeoutMs + 5_000;
      join(syncThread, bound);
      join(flushThread, bound);
      firstLoad.complete(false);
      usage.finish();
    } catch (Throwable failure) {
      reporter.report(failure);
    } finally {
      done.complete(null);
    }
  }

  private static void join(Thread thread, long boundMs) {
    if (thread == Thread.currentThread()) {
      return;
    }
    try {
      thread.join(boundMs);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }
}
