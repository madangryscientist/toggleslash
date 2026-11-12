package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * A ToggleSlash server SDK client. It keeps a ruleset in sync with the server on two daemon threads
 * and evaluates flags locally: {@code variation}, {@code status} and the other reads never do I/O
 * and never throw.
 *
 * <p>Every method is safe to call from many threads at once.
 */
public final class Client implements AutoCloseable {
  private static final ThreadLocal<Boolean> SDK_THREAD = new ThreadLocal<>();
  private static final Object MISMATCH = new Object();

  private final Options options;
  private final Closing closing = new Closing();
  private final Reporter reporter;
  private final Http syncHttp;
  private final Http usageHttp;
  private final Sync sync;
  private final Usage usage;
  private final Thread syncThread;
  private final Thread flushThread;
  private final AtomicBoolean closeStarted = new AtomicBoolean();
  private final CountDownLatch closed = new CountDownLatch(1);

  private Client(Options options) {
    this.options = options;
    this.reporter = new Reporter(options.onError);
    java.net.http.HttpClient httpClient =
        options.httpClient != null ? options.httpClient : Http.newClient(options.requestTimeout);
    this.syncHttp = new Http(httpClient);
    this.usageHttp = new Http(httpClient);
    this.sync = new Sync(options, syncHttp, closing, reporter);
    this.usage =
        new Usage(
            usageHttp,
            options.url,
            options.sdkKey,
            options.requestTimeout,
            options.clock,
            reporter);
    this.syncThread = new Thread(sync, "toggleslash-sync");
    this.syncThread.setDaemon(true);
    this.flushThread = new Thread(this::flushLoop, "toggleslash-flush");
    this.flushThread.setDaemon(true);
  }

  /**
   * Creates a client, starts its two daemon threads and begins the first load at once. It never
   * waits for the network and never throws.
   *
   * @param options the options
   * @return the client
   */
  public static Client create(Options options) {
    Client client = new Client(options);
    try {
      client.syncThread.start();
      client.flushThread.start();
    } catch (Throwable error) {
      client.sync.ready().complete(false);
      client.reporter.report(error);
    }
    return client;
  }

  static void markSdkThread() {
    SDK_THREAD.set(Boolean.TRUE);
  }

  private static boolean onSdkThread() {
    return SDK_THREAD.get() != null;
  }

  private void flushLoop() {
    markSdkThread();
    try {
      while (!closing.isClosed()) {
        if (options.clock.sleep(options.flushInterval.toMillis(), closing)) {
          return;
        }
        try {
          usage.flush(false);
        } catch (RuntimeException e) {
          reporter.report(e);
        }
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  /**
   * Waits for the first load to finish. The answer is fixed once it has: true if a ruleset is held.
   * Bounded by {@code requestTimeout}. From a callback on an SDK thread it returns at once with
   * what is held. After {@code close} it answers false if the load had not finished.
   *
   * @return whether a ruleset was loaded
   */
  public boolean ready() {
    CompletableFuture<Boolean> ready = sync.ready();
    try {
      if (onSdkThread()) {
        return ready.isDone() && ready.getNow(false);
      }
      return ready.get(options.requestTimeout.toMillis() * 2 + 1000, TimeUnit.MILLISECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return ready.isDone() && ready.getNow(false);
    } catch (ExecutionException | TimeoutException | RuntimeException e) {
      return false;
    }
  }

  /**
   * The client's sync state as one snapshot.
   *
   * @return the status
   */
  public Status status() {
    Sync.State state = sync.state();
    return new Status(state.etag(), state.lastSyncAt(), state.stream(), state.lastError());
  }

  /** Sends the pending usage counts now, under the rules of the usage protocol. */
  public void flush() {
    try {
      usage.flush(false);
    } catch (RuntimeException e) {
      reporter.report(e);
    }
  }

  // ---- results ----------------------------------------------------------------------------

  /** Converts a served value for one accessor; {@link #MISMATCH} when it has the wrong kind. */
  private interface Accessor {
    Object convert(Object served);
  }

  private static final Accessor BOOLEAN = served -> served instanceof Boolean ? served : MISMATCH;
  private static final Accessor STRING = served -> served instanceof String ? served : MISMATCH;
  private static final Accessor DOUBLE = served -> served instanceof Double ? served : MISMATCH;
  private static final Accessor LONG = whole(Long.MIN_VALUE, Long.MAX_VALUE, Double::longValue);
  private static final Accessor INTEGER =
      whole(Integer.MIN_VALUE, Integer.MAX_VALUE, v -> (Object) Integer.valueOf(v.intValue()));
  private static final Accessor SHORT =
      whole(Short.MIN_VALUE, Short.MAX_VALUE, v -> (Object) Short.valueOf(v.shortValue()));
  private static final Accessor BYTE =
      whole(Byte.MIN_VALUE, Byte.MAX_VALUE, v -> (Object) Byte.valueOf(v.byteValue()));
  private static final Accessor FLOAT =
      served ->
          served instanceof Double
              ? (Object) Float.valueOf(((Double) served).floatValue())
              : MISMATCH;
  private static final Accessor ANY = served -> served;
  private static final Accessor NEVER = served -> MISMATCH;

  private static Accessor whole(
      double min, double max, java.util.function.Function<Double, Object> convert) {
    return served -> {
      if (!(served instanceof Double)) {
        return MISMATCH;
      }
      double value = (Double) served;
      boolean fits = value == Math.rint(value) && value >= min && value <= max;
      // 2^63 is the double next to Long.MAX_VALUE, and does not fit.
      if (max == Long.MAX_VALUE) {
        fits =
            value == Math.rint(value)
                && value >= -9.223372036854775808E18
                && value < 9.223372036854775808E18;
      }
      return fits ? convert.apply(value) : MISMATCH;
    };
  }

  private static String kindOf(Object value) {
    if (value == null) {
      return "null";
    }
    if (value instanceof Boolean) {
      return "boolean";
    }
    if (value instanceof Number) {
      return "number";
    }
    if (value instanceof String) {
      return "string";
    }
    if (value instanceof List) {
      return "array";
    }
    if (value instanceof Map) {
      return "object";
    }
    return "other";
  }

  private static Accessor sameKindAs(Object fallback) {
    if (fallback == null) {
      return ANY;
    }
    String kind = kindOf(fallback);
    if (kind.equals("other")) {
      return NEVER;
    }
    return served -> kindOf(served).equals(kind) ? served : MISMATCH;
  }

  private static Accessor forFallback(Object fallback) {
    if (fallback instanceof Boolean) {
      return BOOLEAN;
    }
    if (fallback instanceof String) {
      return STRING;
    }
    if (fallback instanceof Long) {
      return LONG;
    }
    if (fallback instanceof Integer) {
      return INTEGER;
    }
    if (fallback instanceof Short) {
      return SHORT;
    }
    if (fallback instanceof Byte) {
      return BYTE;
    }
    if (fallback instanceof Double) {
      return DOUBLE;
    }
    if (fallback instanceof Float) {
      return FLOAT;
    }
    return sameKindAs(fallback);
  }

  @SuppressWarnings("unchecked")
  private <T> Detail<T> detail(String flagKey, Object context, T fallback, Accessor accessor) {
    Sync.State state = sync.state();
    Detail<T> result;
    Integer countedIndex = null;
    Throwable failure = null;
    try {
      if (state.ruleset() == null) {
        result = new Detail<>(fallback, null, Reason.error(ErrorCode.NOT_READY));
      } else {
        Object evaluable = context instanceof Context built ? built.toMap() : context;
        Result evaluated = options.evaluator.evaluate(state.ruleset(), flagKey, evaluable);
        if (evaluated.variationIndex() == null) {
          result = new Detail<>(fallback, null, evaluated.reason());
        } else {
          Object converted = accessor.convert(evaluated.value());
          if (converted == MISMATCH) {
            result = new Detail<>(fallback, null, Reason.error(ErrorCode.TYPE_MISMATCH));
          } else {
            result = new Detail<>((T) converted, evaluated.variationIndex(), evaluated.reason());
            countedIndex = evaluated.variationIndex();
          }
        }
      }
    } catch (Throwable error) {
      failure = error;
      countedIndex = null;
      result = new Detail<>(fallback, null, Reason.error(ErrorCode.EVALUATION_FAILED));
    }
    if (failure != null) {
      reporter.report(failure);
    }
    if (countedIndex != null && !closing.isClosed()) {
      try {
        usage.record(flagKey, countedIndex);
      } catch (RuntimeException e) {
        reporter.report(e);
      }
    }
    runHook(flagKey, context, result, state);
    return result;
  }

  private void runHook(String flagKey, Object context, Detail<?> result, Sync.State state) {
    AfterEvaluation hook = options.afterEvaluation;
    if (hook == null) {
      return;
    }
    try {
      Long version = null;
      Ruleset ruleset = state.ruleset();
      if (ruleset != null) {
        version = ruleset.flagVersion(flagKey);
      }
      hook.accept(
          new EvaluationEvent(
              flagKey,
              context,
              result.value(),
              result.variationIndex(),
              result.reason(),
              state.etag(),
              version));
    } catch (Throwable error) {
      reporter.report(error);
    }
  }

  private <T> Detail<T> safe(String flagKey, Object context, T fallback, Accessor accessor) {
    try {
      return detail(flagKey, context, fallback, accessor);
    } catch (Throwable error) {
      return new Detail<>(fallback, null, Reason.error(ErrorCode.EVALUATION_FAILED));
    }
  }

  // ---- public accessors -------------------------------------------------------------------

  /**
   * A boolean flag.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve a boolean
   * @return the value
   */
  public boolean booleanVariation(String flagKey, Object context, boolean fallback) {
    return booleanVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * A boolean flag with its reason.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve a boolean
   * @return the detail
   */
  public Detail<Boolean> booleanVariationDetail(String flagKey, Object context, boolean fallback) {
    return safe(flagKey, context, fallback, BOOLEAN);
  }

  /**
   * A string flag. A null fallback accepts only a string.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve a string
   * @return the value
   */
  public String stringVariation(String flagKey, Object context, String fallback) {
    return stringVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * A string flag with its reason.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve a string
   * @return the detail
   */
  public Detail<String> stringVariationDetail(String flagKey, Object context, String fallback) {
    return safe(flagKey, context, fallback, STRING);
  }

  /**
   * The integer accessor: a whole number within the signed 64-bit range.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve one
   * @return the value
   */
  public long longVariation(String flagKey, Object context, long fallback) {
    return longVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * The integer accessor with its reason.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve one
   * @return the detail
   */
  public Detail<Long> longVariationDetail(String flagKey, Object context, long fallback) {
    return safe(flagKey, context, fallback, LONG);
  }

  /**
   * A number flag.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve a number
   * @return the value
   */
  public double doubleVariation(String flagKey, Object context, double fallback) {
    return doubleVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * A number flag with its reason.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve a number
   * @return the detail
   */
  public Detail<Double> doubleVariationDetail(String flagKey, Object context, double fallback) {
    return safe(flagKey, context, fallback, DOUBLE);
  }

  /**
   * A flag of any JSON kind: null, Boolean, Double, String, List or Map. A null fallback accepts
   * any served kind; any other fallback accepts only its own kind.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve that kind
   * @return the value
   */
  public Object jsonVariation(String flagKey, Object context, Object fallback) {
    return jsonVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * A flag of any JSON kind with its reason.
   *
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve that kind
   * @return the detail
   */
  public Detail<Object> jsonVariationDetail(String flagKey, Object context, Object fallback) {
    return safe(flagKey, context, fallback, sameKindAs(fallback));
  }

  /**
   * The generic accessor: the fallback's class picks the rules. A {@code Boolean}, a {@code
   * String}, an integer class ({@code Long}, {@code Integer}, {@code Short}, {@code Byte}: a whole
   * number within that class's range), a {@code Double} or {@code Float}, a {@code List}, a {@code
   * Map}, or {@code null} for any kind.
   *
   * @param <T> the fallback's type
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve that kind
   * @return the value
   */
  public <T> T variation(String flagKey, Object context, T fallback) {
    return variationDetail(flagKey, context, fallback).value();
  }

  /**
   * The generic accessor with its reason.
   *
   * @param <T> the fallback's type
   * @param flagKey the flag
   * @param context a {@link Context} or a map tree of one
   * @param fallback the value when the flag cannot serve that kind
   * @return the detail
   */
  public <T> Detail<T> variationDetail(String flagKey, Object context, T fallback) {
    return safe(flagKey, context, fallback, forFallback(fallback));
  }

  // ---- close ------------------------------------------------------------------------------

  /**
   * Stops following the server, stops the flush loop, waits for them, then sends the final usage
   * flush. Safe to call twice and from many threads: every call waits for the same shutdown. Called
   * on an SDK thread it starts the shutdown and returns without waiting. It returns within about
   * twice {@code requestTimeout}.
   */
  @Override
  public void close() {
    boolean first = closeStarted.compareAndSet(false, true);
    if (first) {
      closing.close();
      sync.markClosed();
      syncHttp.abortAll();
      if (onSdkThread()) {
        Thread thread = new Thread(this::shutdown, "toggleslash-close");
        thread.setDaemon(true);
        thread.start();
        return;
      }
      shutdown();
      return;
    }
    if (onSdkThread()) {
      return;
    }
    try {
      closed.await(options.requestTimeout.toMillis() * 4 + 2000, TimeUnit.MILLISECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  private void shutdown() {
    try {
      long bound = options.requestTimeout.toMillis();
      long end = System.nanoTime() + bound * 1_000_000L;
      join(syncThread, end);
      join(flushThread, end);
      try {
        usage.flush(true);
      } catch (RuntimeException e) {
        reporter.report(e);
      }
    } finally {
      closed.countDown();
    }
  }

  private static void join(Thread thread, long endNanos) {
    if (!thread.isAlive() || thread == Thread.currentThread()) {
      return;
    }
    try {
      long left = (endNanos - System.nanoTime()) / 1_000_000L;
      if (left > 0) {
        thread.join(left);
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  /**
   * The bound the SDK keeps on how long a request may take.
   *
   * @return the bound
   */
  Duration requestTimeout() {
    return options.requestTimeout;
  }
}
