package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;

/**
 * The ToggleSlash server SDK. It keeps a ruleset in memory, follows the server, evaluates flags
 * locally and sends usage counts. It follows {@code docs/sdk-contract.md}; the mapping of the
 * canonical names is in the README.
 *
 * <p>No method throws. {@code variation} and {@code status} read memory and do no I/O. A client
 * that is never closed does not keep the JVM alive (its threads are daemon threads).
 */
public final class Client implements AutoCloseable {
  /** The kinds of value an accessor serves. */
  private enum Want {
    BOOL,
    STRING,
    FLOAT,
    INT,
    FALLBACK
  }

  private static final Object MISMATCH = new Object();

  private final Core core;
  private final Usage usage;
  private final SyncLoop sync;
  private final Http syncHttp;
  private final Http usageHttp;
  private final CompletableFuture<Boolean> firstLoad = new CompletableFuture<>();
  private final CountDownLatch done = new CountDownLatch(1);
  private final long timeoutNanos;
  private Thread syncThread;
  private Thread flushThread;

  private Client(Options options) {
    this.core = new Core(options);
    this.timeoutNanos = options.timeoutNanos();
    java.net.http.HttpClient http = Http.newClient(options);
    this.syncHttp = new Http(http, options);
    this.usageHttp = new Http(http, options);
    this.usage = new Usage(core, usageHttp);
    this.sync = new SyncLoop(core, syncHttp);
  }

  /**
   * Builds a client and starts its two daemon threads. It does not wait for the network and does
   * not throw: a URL it cannot use makes each request a {@code network} error.
   *
   * @param options the options (null gives a client that cannot reach any server)
   * @return the client
   */
  public static Client create(Options options) {
    Options use = options == null ? Options.builder("", "").build() : options;
    Client client = new Client(use);
    client.start();
    return client;
  }

  private void start() {
    try {
      syncThread = new SdkThread(this::runSync, "toggleslash-sync");
      flushThread = new SdkThread(this::runFlush, "toggleslash-flush");
      syncThread.start();
      flushThread.start();
    } catch (Throwable e) {
      core.report(e);
      firstLoad.complete(false);
    }
  }

  private void runSync() {
    try {
      sync.firstLoad();
    } catch (Throwable e) {
      core.report(e);
    } finally {
      firstLoad.complete(core.state.get().ruleset() != null);
    }
    try {
      sync.run();
    } catch (Throwable e) {
      core.report(e);
    }
  }

  private void runFlush() {
    try {
      while (!core.closing.get()) {
        core.pacer.sleep(core.closed, core.options.flushNanos());
        if (core.closing.get()) {
          return;
        }
        usage.flush(false);
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    } catch (Throwable e) {
      core.report(e);
    }
  }

  /**
   * Waits for the first ruleset load to finish and answers whether a ruleset is held. The answer is
   * fixed when the first attempt settles and is bounded by {@code requestTimeout}. Called on one of
   * the SDK's own threads (from a callback) it answers at once with the state held. After {@code
   * close} it answers false if it had not settled.
   *
   * @return true if a ruleset has loaded
   */
  public boolean ready() {
    try {
      boolean held = core.state.get().ruleset() != null;
      if (Thread.currentThread() instanceof SdkThread) {
        return firstLoad.getNow(held);
      }
      return firstLoad.get();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return core.state.get().ruleset() != null;
    } catch (Throwable e) {
      return false;
    }
  }

  /**
   * What the client has loaded and how its stream is doing, as one snapshot.
   *
   * @return the status
   */
  public Status status() {
    State s = core.state.get();
    return new Status(s.etag(), s.lastSyncAt(), s.stream(), s.lastError());
  }

  /**
   * A boolean flag.
   *
   * @param key the flag key
   * @param context the context, in the JSON shape of {@code evaluation.md}
   * @param fallback the value when the flag cannot be served
   * @return the value
   */
  public boolean boolVariation(String key, Object context, boolean fallback) {
    return boolVariationDetail(key, context, fallback).value();
  }

  /**
   * Like {@link #boolVariation}, with the variation index and the reason.
   *
   * @param key the flag key
   * @param context the context
   * @param fallback the value when the flag cannot be served
   * @return the detail
   */
  public Detail<Boolean> boolVariationDetail(String key, Object context, boolean fallback) {
    return detail(key, context, fallback, Want.BOOL);
  }

  /**
   * A string flag.
   *
   * @param key the flag key
   * @param context the context
   * @param fallback the value when the flag cannot be served; may be null
   * @return the value
   */
  public String stringVariation(String key, Object context, String fallback) {
    return stringVariationDetail(key, context, fallback).value();
  }

  /**
   * Like {@link #stringVariation}, with the variation index and the reason.
   *
   * @param key the flag key
   * @param context the context
   * @param fallback the value when the flag cannot be served; may be null
   * @return the detail
   */
  public Detail<String> stringVariationDetail(String key, Object context, String fallback) {
    return detail(key, context, fallback, Want.STRING);
  }

  /**
   * A number flag, as a double.
   *
   * @param key the flag key
   * @param context the context
   * @param fallback the value when the flag cannot be served
   * @return the value
   */
  public double floatVariation(String key, Object context, double fallback) {
    return floatVariationDetail(key, context, fallback).value();
  }

  /**
   * Like {@link #floatVariation}, with the variation index and the reason.
   *
   * @param key the flag key
   * @param context the context
   * @param fallback the value when the flag cannot be served
   * @return the detail
   */
  public Detail<Double> floatVariationDetail(String key, Object context, double fallback) {
    return detail(key, context, fallback, Want.FLOAT);
  }

  /**
   * A number flag, as a {@code long}. A served number with a fractional part, or outside the range
   * of {@code long}, is a {@code TYPE_MISMATCH} and the caller gets the fallback. {@code 2.0} is 2.
   *
   * @param key the flag key
   * @param context the context
   * @param fallback the value when the flag cannot be served
   * @return the value
   */
  public long intVariation(String key, Object context, long fallback) {
    return intVariationDetail(key, context, fallback).value();
  }

  /**
   * Like {@link #intVariation}, with the variation index and the reason.
   *
   * @param key the flag key
   * @param context the context
   * @param fallback the value when the flag cannot be served
   * @return the detail
   */
  public Detail<Long> intVariationDetail(String key, Object context, long fallback) {
    return detail(key, context, fallback, Want.INT);
  }

  /**
   * Any flag, by the kind of the fallback: a {@link Boolean}, a {@link Number}, a {@link String}, a
   * {@link List} or a {@link Map}. A null fallback accepts any served value. A served number is a
   * {@link Double}, a served array a {@link List} and a served object a {@link Map}, none of which
   * can be changed.
   *
   * @param key the flag key
   * @param context the context
   * @param fallback the value when the flag cannot be served
   * @return the value
   */
  public Object variation(String key, Object context, Object fallback) {
    return variationDetail(key, context, fallback).value();
  }

  /**
   * Like {@link #variation}, with the variation index and the reason.
   *
   * @param key the flag key
   * @param context the context
   * @param fallback the value when the flag cannot be served
   * @return the detail
   */
  public Detail<Object> variationDetail(String key, Object context, Object fallback) {
    return detail(key, context, fallback, Want.FALLBACK);
  }

  @SuppressWarnings("unchecked")
  private <T> Detail<T> detail(String key, Object context, Object fallback, Want want) {
    State state = core.state.get();
    Detail<?> result;
    boolean counted = false;
    try {
      Ruleset ruleset = state.ruleset();
      if (ruleset == null) {
        result = new Detail<>(fallback, null, Reason.error(ErrorCode.NOT_READY));
      } else {
        Result evaluated = core.options.evaluate().apply(ruleset, key, context);
        if (evaluated.variationIndex() == null) {
          result = new Detail<>(fallback, null, evaluated.reason());
        } else {
          Object served = coerce(want, evaluated.value(), fallback);
          if (served == MISMATCH) {
            result = new Detail<>(fallback, null, Reason.error(ErrorCode.TYPE_MISMATCH));
          } else {
            result = new Detail<>(served, evaluated.variationIndex(), evaluated.reason());
            counted = true;
          }
        }
      }
    } catch (Throwable e) {
      core.report(e);
      result = new Detail<>(fallback, null, Reason.error(ErrorCode.EVALUATION_FAILED));
    }
    if (counted) {
      count(key, result.variationIndex());
    }
    runHook(key, context, result, state);
    return (Detail<T>) result;
  }

  private void count(String key, Integer index) {
    try {
      usage.record(key, index);
    } catch (Throwable e) {
      core.report(e);
    }
  }

  private void runHook(String key, Object context, Detail<?> result, State state) {
    Consumer<EvaluationEvent> hook = core.options.afterEvaluation();
    if (hook == null) {
      return;
    }
    try {
      Ruleset ruleset = state.ruleset();
      Long version = ruleset == null ? null : ruleset.flagVersion(key);
      hook.accept(
          new EvaluationEvent(
              key,
              context,
              result.value(),
              result.variationIndex(),
              result.reason(),
              state.etag(),
              version));
    } catch (Throwable e) {
      core.report(e);
    }
  }

  /**
   * The served value as the accessor's type, or {@link #MISMATCH}. The mismatch rule is section 4.
   */
  private static Object coerce(Want want, Object served, Object fallback) {
    switch (want) {
      case BOOL:
        return served instanceof Boolean || (served == null && fallback == null)
            ? served
            : MISMATCH;
      case STRING:
        return served instanceof String || (served == null && fallback == null) ? served : MISMATCH;
      case FLOAT:
        return served instanceof Double ? served : MISMATCH;
      case INT:
        return asLong(served);
      default:
        return sameKind(served, fallback) ? served : MISMATCH;
    }
  }

  /**
   * The long a served number spells, or {@link #MISMATCH} for a fraction or a value out of range.
   */
  private static Object asLong(Object served) {
    if (!(served instanceof Double number)) {
      return MISMATCH;
    }
    double d = number;
    boolean whole = d == Math.rint(d) && !Double.isInfinite(d);
    // 2^63 is the first double above the range; -2^63 is in it.
    boolean inRange = d >= -0x1p63 && d < 0x1p63;
    return whole && inRange ? (Object) (long) d : MISMATCH;
  }

  private static boolean sameKind(Object served, Object fallback) {
    if (fallback == null) {
      return true;
    }
    if (served == null) {
      return false;
    }
    if (fallback instanceof Boolean) {
      return served instanceof Boolean;
    }
    if (fallback instanceof Number) {
      return served instanceof Number;
    }
    if (fallback instanceof String) {
      return served instanceof String;
    }
    if (fallback instanceof List) {
      return served instanceof List;
    }
    if (fallback instanceof Map) {
      return served instanceof Map;
    }
    return false;
  }

  /**
   * Sends the pending usage counts now, under the rules of section 9 of the contract. It never
   * throws. After {@code close} it does nothing.
   */
  public void flush() {
    try {
      if (!core.closing.get()) {
        usage.flush(false);
      }
    } catch (Throwable e) {
      core.report(e);
    }
  }

  /**
   * Stops following the server, sends the pending usage counts and settles {@code ready} as false
   * if it had not settled. Safe to call twice or from many threads: every call waits for the same
   * shutdown. Called on one of the SDK's own threads it starts the shutdown and returns without
   * waiting. It returns within about twice {@code requestTimeout} even when a request hangs.
   */
  @Override
  public void close() {
    try {
      boolean first = core.closing.compareAndSet(false, true);
      boolean own = Thread.currentThread() instanceof SdkThread;
      if (first && own) {
        new SdkThread(this::shutdown, "toggleslash-close").start();
      } else if (first) {
        shutdown();
      } else if (!own) {
        awaitDone();
      }
    } catch (Throwable e) {
      core.report(e);
    }
  }

  private void awaitDone() {
    boolean interrupted = false;
    while (true) {
      try {
        done.await();
        break;
      } catch (InterruptedException e) {
        interrupted = true;
      }
    }
    if (interrupted) {
      Thread.currentThread().interrupt();
    }
  }

  private void shutdown() {
    try {
      core.state.updateAndGet(s -> s.withStream(StreamState.CLOSED));
      usage.stop();
      core.closed.countDown();
      firstLoad.complete(false);
      syncHttp.seal();
      join(syncThread, timeoutNanos);
      join(flushThread, timeoutNanos);
      if (flushThread != null && flushThread.isAlive()) {
        usageHttp.abortAll();
        join(flushThread, TimeUnit.SECONDS.toNanos(1));
      }
      usage.flush(true);
    } catch (Throwable e) {
      core.report(e);
    } finally {
      done.countDown();
    }
  }

  private static void join(Thread thread, long nanos) {
    if (thread == null || thread == Thread.currentThread()) {
      return;
    }
    try {
      thread.join(Math.max(1, TimeUnit.NANOSECONDS.toMillis(nanos)));
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }
}
