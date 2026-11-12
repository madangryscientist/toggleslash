package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.time.Instant;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.Consumer;

/**
 * A ToggleSlash server SDK client: it keeps a ruleset current from the server and evaluates flags
 * against it locally, without I/O and without throwing.
 *
 * <p>Create one with {@link #create(Options)}, keep it for the life of the process and call {@link
 * #close()} on shutdown. Every method is safe to call from any thread.
 */
public final class Client implements AutoCloseable {
  private enum Kind {
    BOOL,
    STRING,
    NUMBER,
    INT,
    JSON
  }

  private static final double TWO_POW_63 = 9.223372036854775808E18;

  private final Config config;
  private final Store store;
  private final Http http;
  private final Sync sync;
  private final Usage usage;
  private final Options.Evaluation evaluation;
  private final Consumer<EvaluationEvent> hook;
  private final CompletableFuture<Boolean> first = new CompletableFuture<>();
  private final Thread syncThread;
  private final Thread flushThread;
  private final AtomicBoolean shutdownStarted = new AtomicBoolean();
  private final CountDownLatch done = new CountDownLatch(1);

  private Client(Options options) {
    this.config = Config.of(options);
    this.store = new Store(config.stream(), options.onError);
    this.http = new Http(config);
    this.sync = new Sync(config, http, store);
    this.usage = new Usage(config, http, store);
    this.evaluation = options.evaluation;
    this.hook = options.afterEvaluation;
    this.syncThread =
        new SdkThread(
            () -> sync.run(() -> first.complete(store.get().ruleset() != null)),
            "toggleslash-sync");
    this.flushThread = new SdkThread(usage::loop, "toggleslash-flush");
  }

  /**
   * Creates a client and starts loading the ruleset in the background. It returns at once and never
   * throws; until the first load has finished, evaluations answer their fallback with {@code
   * NOT_READY}.
   *
   * @param options the options
   * @return the client
   */
  public static Client create(Options options) {
    Client client = new Client(options == null ? new Options() : options);
    client.syncThread.start();
    client.flushThread.start();
    return client;
  }

  /**
   * Waits for the first ruleset load to finish, at most about the request timeout.
   *
   * @return true if a ruleset is loaded; false means flags serve fallbacks until the SDK reaches
   *     the server. On one of the SDK's own threads (a hook or an error callback) it does not wait
   *     and answers whether a ruleset is loaded now.
   */
  public boolean ready() {
    if (Thread.currentThread() instanceof SdkThread) {
      return store.get().ruleset() != null;
    }
    try {
      return first.get();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return store.get().ruleset() != null;
    } catch (java.util.concurrent.ExecutionException e) {
      return false;
    }
  }

  /**
   * A future that completes once the first ruleset load has finished. It completes normally and
   * never exceptionally.
   *
   * @return the future: true if a ruleset is loaded
   */
  public CompletableFuture<Boolean> readyFuture() {
    return first.copy();
  }

  /**
   * A boolean flag's value for a context.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served
   * @return the served value or the fallback
   */
  public boolean boolVariation(String flagKey, Context context, boolean fallback) {
    return boolVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #boolVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served
   * @return the answer, the variation index and the reason
   */
  public Detail<Boolean> boolVariationDetail(String flagKey, Context context, boolean fallback) {
    return detail(flagKey, context, fallback, Kind.BOOL);
  }

  /**
   * A string flag's value for a context.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served; it may be null
   * @return the served value or the fallback
   */
  public String stringVariation(String flagKey, Context context, String fallback) {
    return stringVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #stringVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served; it may be null
   * @return the answer, the variation index and the reason
   */
  public Detail<String> stringVariationDetail(String flagKey, Context context, String fallback) {
    return detail(flagKey, context, fallback, Kind.STRING);
  }

  /**
   * A number flag's value for a context.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served
   * @return the served value or the fallback
   */
  public double doubleVariation(String flagKey, Context context, double fallback) {
    return doubleVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #doubleVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served
   * @return the answer, the variation index and the reason
   */
  public Detail<Double> doubleVariationDetail(String flagKey, Context context, double fallback) {
    return detail(flagKey, context, fallback, Kind.NUMBER);
  }

  /**
   * An integer flag's value for a context. A served number with a fractional part, or outside the
   * range of {@code long}, is a type mismatch and gives the fallback.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served
   * @return the served value or the fallback
   */
  public long longVariation(String flagKey, Context context, long fallback) {
    return longVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #longVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served
   * @return the answer, the variation index and the reason
   */
  public Detail<Long> longVariationDetail(String flagKey, Context context, long fallback) {
    return detail(flagKey, context, fallback, Kind.INT);
  }

  /**
   * A flag's value for a context, as JSON, whatever its kind. A null or {@link Json#NULL} fallback
   * accepts a value of any kind; any other fallback must be the same kind as the value (arrays and
   * objects are different kinds), compared by kind and never by contents.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served
   * @return the served value or the fallback
   */
  public Json jsonVariation(String flagKey, Context context, Json fallback) {
    return jsonVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #jsonVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the answer when nothing usable is served
   * @return the answer, the variation index and the reason
   */
  public Detail<Json> jsonVariationDetail(String flagKey, Context context, Json fallback) {
    return detail(flagKey, context, fallback, Kind.JSON);
  }

  /**
   * What this SDK has loaded and how its stream is doing. It reads memory, makes no network call
   * and never throws.
   *
   * @return the status
   */
  public Status status() {
    Store.State state = store.get();
    return new Status(
        state.etag(),
        state.lastSyncMs() == null ? null : Instant.ofEpochMilli(state.lastSyncMs()),
        state.stream(),
        state.lastError());
  }

  /**
   * Stops following the server and sends the pending usage counts. It never throws, is safe to call
   * twice and from several threads at once, and returns within about twice the request timeout.
   * Called from one of the SDK's own threads (a hook or an error callback) it starts the shutdown
   * and returns at once.
   */
  @Override
  public void close() {
    if (Thread.currentThread() instanceof SdkThread) {
      new SdkThread(this::shutDown, "toggleslash-close").start();
      return;
    }
    shutDown();
    try {
      done.await();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  private void shutDown() {
    if (!shutdownStarted.compareAndSet(false, true)) {
      return;
    }
    try {
      store.close();
      sync.stop();
      usage.stop();
      long bound = config.timeoutMs() + 1_000;
      join(syncThread, bound);
      join(flushThread, bound);
      first.complete(false);
      usage.finish();
    } catch (Throwable t) {
      store.report(t);
    } finally {
      done.countDown();
    }
  }

  private static void join(Thread thread, long ms) {
    if (thread == Thread.currentThread()) {
      return;
    }
    try {
      thread.join(ms);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  // Package-private reads for this package's tests.
  Hold syncHold() {
    return sync.hold();
  }

  Hold usageHold() {
    return usage.hold();
  }

  boolean awaitClosed(long ms) throws InterruptedException {
    return done.await(ms, TimeUnit.MILLISECONDS);
  }

  private static <T> Detail<T> failed(T fallback, ErrorCode code) {
    return new Detail<>(fallback, null, Reason.error(code));
  }

  private <T> Detail<T> detail(String flagKey, Context context, T fallback, Kind kind) {
    Store.State state = store.get();
    Detail<T> result;
    try {
      result = compute(state.ruleset(), flagKey, context, fallback, kind);
    } catch (Throwable t) {
      store.report(t);
      result = failed(fallback, ErrorCode.EVALUATION_FAILED);
    }
    if (hook != null) {
      callHook(state, flagKey, context, result);
    }
    return result;
  }

  private void callHook(Store.State state, String flagKey, Context context, Detail<?> result) {
    try {
      Ruleset ruleset = state.ruleset();
      Long version = ruleset == null ? null : ruleset.flagVersion(flagKey);
      hook.accept(
          new EvaluationEvent(
              flagKey,
              context,
              result.value(),
              result.variationIndex(),
              result.reason(),
              state.etag(),
              version));
    } catch (Throwable t) {
      store.report(t);
    }
  }

  @SuppressWarnings("unchecked")
  private <T> Detail<T> compute(
      Ruleset ruleset, String flagKey, Context context, T fallback, Kind kind) {
    if (ruleset == null) {
      return failed(fallback, ErrorCode.NOT_READY);
    }
    Json json = context == null ? Json.NULL : context.json();
    Result served = evaluation.evaluate(ruleset, flagKey, json);
    if (served.variationIndex() == null) {
      return new Detail<>(fallback, null, served.reason());
    }
    Object converted = convert(served.value(), kind, fallback);
    if (converted == null) {
      return failed(fallback, ErrorCode.TYPE_MISMATCH);
    }
    usage.record(flagKey, served.variationIndex());
    return new Detail<>((T) converted, served.variationIndex(), served.reason());
  }

  /** The served value as the accessor's Java type, or null when its kind is not the caller's. */
  private static Object convert(Json served, Kind kind, Object fallback) {
    switch (kind) {
      case BOOL:
        return served instanceof Json.Bool b ? Boolean.valueOf(b.value()) : null;
      case STRING:
        return served instanceof Json.Str s ? s.value() : null;
      case NUMBER:
        return served instanceof Json.Num n ? Double.valueOf(n.value()) : null;
      case INT:
        if (served instanceof Json.Num n) {
          double d = n.value();
          if (d == Math.rint(d) && d >= -TWO_POW_63 && d < TWO_POW_63) {
            return Long.valueOf((long) d);
          }
        }
        return null;
      default:
        if (fallback == null || fallback instanceof Json.Null) {
          return served;
        }
        return served.getClass() == fallback.getClass() ? served : null;
    }
  }
}
