package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Evaluator;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * The ToggleSlash server SDK for one environment: it keeps the ruleset current and evaluates flags
 * locally, to {@code docs/evaluation.md} and {@code docs/sdk-contract.md}. Make one with {@link
 * #create(Options)}.
 *
 * <p>Nothing here throws at a caller. {@code boolVariation} and its siblings never do I/O and never
 * wait. A client that is never closed does not keep the JVM alive (its two threads are daemons).
 * Every method is safe to call from many threads at once.
 */
public final class Client implements AutoCloseable {
  /** Standing in for a served value of the wrong kind. */
  private static final Object MISMATCH = new Object();

  private static final double TWO_63 = 9.223372036854775808E18;

  private enum Kind {
    BOOL,
    STRING,
    LONG,
    NUMBER,
    JSON
  }

  /** The SDK's own threads; a callback running on one must not wait for itself. */
  private static final class SdkThread extends Thread {
    private final Client owner;

    SdkThread(Client owner, Runnable body, String name) {
      super(body, name);
      this.owner = owner;
      setDaemon(true);
    }
  }

  /** For tests: the evaluator, so a test can make it raise. */
  interface Evaluate {
    Result evaluate(Ruleset ruleset, String flagKey, Object context);
  }

  private final Options options;
  private final Evaluate evaluate;
  private final Sync sync;
  private final Usage usage;
  private final SdkThread syncThread;
  private final SdkThread flushThread;
  private final AtomicBoolean closeStarted = new AtomicBoolean();
  private final CompletableFuture<Void> closed = new CompletableFuture<>();
  private volatile boolean closing;

  private Client(Options options, Evaluate evaluate) {
    this.options = options;
    this.evaluate = evaluate;
    HttpCalls http = new HttpCalls(options.url, options.sdkKey, options.requestTimeout);
    this.sync = new Sync(options, http, this::report);
    this.usage = new Usage(http, options.clock, this::report);
    this.syncThread = new SdkThread(this, sync::run, "toggleslash-sync");
    this.flushThread = new SdkThread(this, this::flushLoop, "toggleslash-flush");
  }

  /**
   * Builds a client, starts its two daemon threads and returns at once, without waiting for the
   * network. It never throws.
   *
   * @param options the options; null means all defaults (which cannot reach a server)
   * @return the client
   */
  public static Client create(Options options) {
    return create(options, Evaluator::evaluate);
  }

  static Client create(Options options, Evaluate evaluate) {
    Client client = new Client(options == null ? Options.builder().build() : options, evaluate);
    client.syncThread.start();
    client.flushThread.start();
    return client;
  }

  /**
   * Waits for the first ruleset load to settle (bounded by the request timeout) and says whether a
   * ruleset is held. The answer is fixed when the first attempt settles. Called on one of the SDK's
   * own threads (from a callback) it returns at once with whether a ruleset is held. After {@link
   * #close} it answers false if it had not settled.
   *
   * @return true if the first load worked
   */
  public boolean ready() {
    if (onOwnThread()) {
      return sync.firstLoad.isDone() ? sync.firstLoad.getNow(false) : sync.snapshot() != null;
    }
    long bound = 2 * options.requestTimeout.toMillis() + 1_000L;
    try {
      return sync.firstLoad.get(bound, TimeUnit.MILLISECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return sync.snapshot() != null;
    } catch (ExecutionException | TimeoutException e) {
      return sync.snapshot() != null;
    }
  }

  /**
   * What the SDK has loaded and how its stream is doing: one consistent snapshot, read from memory.
   *
   * @return the status
   */
  public Status status() {
    return sync.status();
  }

  // ---- variation ----

  /** The flag's boolean value for a context, or {@code fallback}. */
  public boolean boolVariation(String flagKey, Context context, boolean fallback) {
    return (Boolean) run(flagKey, context, Kind.BOOL, fallback).value();
  }

  /** Like {@link #boolVariation}, with the variation index and the reason. */
  public Detail<Boolean> boolVariationDetail(String flagKey, Context context, boolean fallback) {
    return typed(run(flagKey, context, Kind.BOOL, fallback));
  }

  /** The flag's string value for a context, or {@code fallback} (which may be null). */
  public String stringVariation(String flagKey, Context context, String fallback) {
    return (String) run(flagKey, context, Kind.STRING, fallback).value();
  }

  /** Like {@link #stringVariation}, with the variation index and the reason. */
  public Detail<String> stringVariationDetail(String flagKey, Context context, String fallback) {
    return typed(run(flagKey, context, Kind.STRING, fallback));
  }

  /**
   * The flag's value as a signed 64-bit integer, or {@code fallback}. A served number with a
   * fractional part, or outside the range of a {@code long}, is a TYPE_MISMATCH and gives the
   * fallback. {@code 2.0} is 2.
   */
  public long longVariation(String flagKey, Context context, long fallback) {
    return (Long) run(flagKey, context, Kind.LONG, fallback).value();
  }

  /** Like {@link #longVariation}, with the variation index and the reason. */
  public Detail<Long> longVariationDetail(String flagKey, Context context, long fallback) {
    return typed(run(flagKey, context, Kind.LONG, fallback));
  }

  /** The flag's number as a {@code double}, or {@code fallback}. */
  public double numberVariation(String flagKey, Context context, double fallback) {
    return (Double) run(flagKey, context, Kind.NUMBER, fallback).value();
  }

  /** Like {@link #numberVariation}, with the variation index and the reason. */
  public Detail<Double> numberVariationDetail(String flagKey, Context context, double fallback) {
    return typed(run(flagKey, context, Kind.NUMBER, fallback));
  }

  /**
   * The flag's value of any kind. The result is null, a {@code Boolean}, a {@code Double}, a {@code
   * String}, or an unmodifiable {@code List} or {@code Map}. A null fallback accepts any served
   * value; a non-null one must be of the same JSON kind (boolean, number, string, array, object) as
   * the served value.
   */
  public Object jsonVariation(String flagKey, Context context, Object fallback) {
    return run(flagKey, context, Kind.JSON, fallback).value();
  }

  /** Like {@link #jsonVariation}, with the variation index and the reason. */
  public Detail<Object> jsonVariationDetail(String flagKey, Context context, Object fallback) {
    return run(flagKey, context, Kind.JSON, fallback);
  }

  @SuppressWarnings("unchecked")
  private static <T> Detail<T> typed(Detail<Object> detail) {
    return (Detail<T>) detail;
  }

  private Detail<Object> run(String flagKey, Context context, Kind kind, Object fallback) {
    Snapshot snapshot = sync.snapshot();
    Detail<Object> detail;
    try {
      detail = compute(snapshot, flagKey, context, kind, fallback);
    } catch (RuntimeException | StackOverflowError e) {
      report(e);
      detail = failed(fallback, ErrorCode.EVALUATION_FAILED);
    }
    if (options.afterEvaluation != null) {
      runHook(snapshot, flagKey, context, detail);
    }
    return detail;
  }

  private Detail<Object> compute(
      Snapshot snapshot, String flagKey, Context context, Kind kind, Object fallback) {
    if (snapshot == null) {
      return failed(fallback, ErrorCode.NOT_READY);
    }
    Result result =
        evaluate.evaluate(snapshot.ruleset(), flagKey, context == null ? null : context.value());
    if (result.variationIndex() == null) {
      return new Detail<>(fallback, null, result.reason());
    }
    Object served = convert(kind, result.value(), fallback);
    if (served == MISMATCH) {
      return failed(fallback, ErrorCode.TYPE_MISMATCH);
    }
    if (!closing) {
      usage.record(flagKey, result.variationIndex());
    }
    return new Detail<>(served, result.variationIndex(), result.reason());
  }

  private static Detail<Object> failed(Object fallback, ErrorCode code) {
    return new Detail<>(fallback, null, Reason.error(code));
  }

  /** The served value as the accessor's type, or {@link #MISMATCH}. */
  private static Object convert(Kind kind, Object value, Object fallback) {
    switch (kind) {
      case BOOL:
        return value instanceof Boolean ? value : MISMATCH;
      case STRING:
        return value instanceof String ? value : MISMATCH;
      case NUMBER:
        return value instanceof Double ? value : MISMATCH;
      case LONG:
        if (value instanceof Double d
            && !d.isInfinite()
            && !d.isNaN()
            && d == Math.rint(d)
            && d >= -TWO_63
            && d < TWO_63) {
          return (long) d.doubleValue();
        }
        return MISMATCH;
      default:
        if (fallback == null || sameJsonKind(value, fallback)) {
          return freeze(value);
        }
        return MISMATCH;
    }
  }

  private static boolean sameJsonKind(Object value, Object fallback) {
    if (value == null) {
      return false;
    }
    return jsonKind(value) != null && jsonKind(value).equals(jsonKind(fallback));
  }

  private static String jsonKind(Object value) {
    if (value instanceof Boolean) {
      return "boolean";
    }
    if (value instanceof Number) {
      return "number";
    }
    if (value instanceof CharSequence) {
      return "string";
    }
    if (value instanceof List<?> || value instanceof Object[]) {
      return "array";
    }
    if (value instanceof Map<?, ?>) {
      return "object";
    }
    return null;
  }

  /**
   * A deep unmodifiable copy of a served array or object, so a caller cannot change the ruleset
   * held.
   */
  private static Object freeze(Object value) {
    if (value instanceof List<?> list) {
      List<Object> copy = new ArrayList<>(list.size());
      for (Object item : list) {
        copy.add(freeze(item));
      }
      return Collections.unmodifiableList(copy);
    }
    if (value instanceof Map<?, ?> map) {
      Map<String, Object> copy = new LinkedHashMap<>();
      for (Map.Entry<?, ?> entry : map.entrySet()) {
        copy.put((String) entry.getKey(), freeze(entry.getValue()));
      }
      return Collections.unmodifiableMap(copy);
    }
    return value;
  }

  private void runHook(Snapshot snapshot, String flagKey, Context context, Detail<Object> detail) {
    try {
      Long version = null;
      if (snapshot != null) {
        Double raw = snapshot.ruleset().flagVersion(flagKey);
        if (raw != null && raw >= -TWO_63 && raw < TWO_63) {
          version = (long) raw.doubleValue();
        }
      }
      options.afterEvaluation.accept(
          new EvaluationEvent(
              flagKey,
              context,
              detail.value(),
              detail.variationIndex(),
              detail.reason(),
              snapshot == null ? null : snapshot.etag(),
              version));
    } catch (Throwable t) { // a hook never reaches the caller, whatever it throws
      report(t);
    }
  }

  // ---- usage ----

  /**
   * Sends the pending usage counts now, under the rules of section 9 of the contract (serial with
   * the background flush, held back by a {@code Retry-After}). It never throws.
   */
  public void flush() {
    usage.flush();
  }

  private void flushLoop() {
    long interval = options.flushInterval.toMillis();
    try {
      while (!closing) {
        if (options.clock.sleep(interval, sync.stop) || closing) {
          return;
        }
        usage.flush();
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  // ---- close ----

  /**
   * Stops following the server, stops the flush loop, waits for both, and sends the pending usage
   * counts. It never fails and is safe to call twice or from many threads: every call returns when
   * the same shutdown is done. Called on an SDK thread (from a callback) it stops the rest and
   * returns without waiting for itself. It returns within about twice the request timeout even when
   * a request hangs.
   */
  @Override
  public void close() {
    if (closeStarted.compareAndSet(false, true)) {
      try {
        shutDown();
      } finally {
        closed.complete(null);
      }
    } else if (!onOwnThread()) {
      closed.join();
    }
  }

  private void shutDown() {
    closing = true;
    sync.halt();
    if (Thread.currentThread() != syncThread) {
      syncThread.interrupt();
    }
    long bound = options.requestTimeout.toMillis() + 1_000L;
    join(syncThread, bound);
    join(flushThread, bound);
    sync.firstLoad.complete(false);
    try {
      usage.finish();
    } catch (RuntimeException e) {
      report(e);
    }
  }

  private static void join(Thread thread, long millis) {
    if (thread == Thread.currentThread()) {
      return;
    }
    try {
      thread.join(millis);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  private boolean onOwnThread() {
    return Thread.currentThread() instanceof SdkThread t && t.owner == this;
  }

  /**
   * Hands a failure to {@code onError}. Whatever it throws is ignored, and no SDK lock is held
   * here.
   */
  private void report(Throwable error) {
    if (options.onError == null) {
      return;
    }
    try {
      options.onError.accept(error);
    } catch (Throwable ignored) { // a faulty error hook must not break the SDK
      // ignored
    }
  }
}
