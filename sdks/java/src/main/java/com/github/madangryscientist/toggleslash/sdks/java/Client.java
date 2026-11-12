package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.Consumer;

/**
 * The ToggleSlash server SDK: it keeps a ruleset current, evaluates flags locally and counts what
 * it served.
 *
 * <p>Everything here is safe to call from many threads, and nothing throws at the caller. {@link
 * #variation} and {@link #variationDetail} read memory only: they never wait for the network. A
 * client that is never closed does not keep the process alive, since its threads are daemons.
 */
public final class Client implements AutoCloseable {
  private static final Object MISMATCH = new Object();

  /** A thread the SDK started, so a callback can tell that it runs on one. */
  private static final class SdkThread extends Thread {
    SdkThread(Runnable body, String name) {
      super(body, name);
      setDaemon(true);
    }
  }

  /** Turns the value the flag serves into what the caller asked for, or {@link #MISMATCH}. */
  @FunctionalInterface
  private interface Converter {
    Object convert(Object served, Object fallback);
  }

  private final Options options;
  private final Consumer<Throwable> onError;
  private final Http http;
  private final Sync sync;
  private final Usage usage;
  private final Wake flushWake = new Wake();
  private final CountDownLatch settled = new CountDownLatch(1);
  private final Object settleLock = new Object();
  private final AtomicBoolean closeStarted = new AtomicBoolean();
  private final CountDownLatch closeDone = new CountDownLatch(1);
  private Thread syncThread;
  private Thread flushThread;
  private volatile boolean firstResult;
  private volatile boolean closing;

  private Client(Options options) {
    this.options = options;
    Consumer<Throwable> user = options.onError();
    this.onError =
        error -> {
          if (user == null) {
            return;
          }
          try {
            user.accept(error);
          } catch (RuntimeException | StackOverflowError ignored) {
            // A faulty error callback must not break the SDK.
          }
        };
    this.http = new Http(options);
    this.sync = new Sync(http, options, onError);
    this.usage = new Usage(http, options, onError);
  }

  /**
   * Builds a client and starts its loops. It does not wait for the network and never throws: a
   * missing or unusable URL makes each request fail with a {@code network} error instead.
   *
   * @param options the options
   * @return the running client
   */
  public static Client create(Options options) {
    Client client = new Client(options == null ? Options.builder().build() : options);
    client.start();
    return client;
  }

  private void start() {
    syncThread = new SdkThread(this::syncBody, "toggleslash-sync");
    flushThread = new SdkThread(this::flushBody, "toggleslash-flush");
    syncThread.start();
    flushThread.start();
  }

  private void syncBody() {
    try {
      sync.run(this::settle);
    } catch (RuntimeException | StackOverflowError failure) {
      onError.accept(failure);
    } finally {
      settle();
    }
  }

  private void flushBody() {
    while (!flushWake.isWoken()) {
      flushWake.sleepUntil(System.nanoTime() + options.flushNanos());
      if (flushWake.isWoken()) {
        return;
      }
      try {
        usage.flush();
      } catch (RuntimeException | StackOverflowError failure) {
        onError.accept(failure);
      }
    }
  }

  /** Fixes the answer of {@link #ready()} the first time it is called. */
  private void settle() {
    synchronized (settleLock) {
      if (settled.getCount() > 0) {
        firstResult = sync.view().ruleset() != null && !closing;
        settled.countDown();
      }
    }
  }

  /**
   * Waits for the first ruleset load to finish, bounded by the request timeout. The answer is fixed
   * at that moment. Called on one of the SDK's own threads (from a callback) it does not wait and
   * answers from what is held.
   *
   * @return true if a ruleset has loaded, false if not or if the client closed before the load
   *     finished
   */
  public boolean ready() {
    if (settled.getCount() > 0) {
      if (Thread.currentThread() instanceof SdkThread) {
        return sync.view().ruleset() != null;
      }
      try {
        settled.await();
      } catch (InterruptedException interrupted) {
        Thread.currentThread().interrupt();
        return false;
      }
    }
    return firstResult;
  }

  /**
   * The value a flag serves for a context, or the fallback. The value is of the fallback's JSON
   * kind (a number comes as {@code Double}, or as the class of a {@code Float}, {@code Integer} or
   * {@code Long} fallback); a {@code null} fallback accepts any kind.
   *
   * @param <T> the type of the fallback
   * @param flagKey the flag
   * @param context a context map, see {@link Context}
   * @param fallback what to return when nothing usable is served
   * @return the served value or the fallback
   */
  public <T> T variation(String flagKey, Object context, T fallback) {
    return variationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #variation}, with the variation index and the reason.
   *
   * @param <T> the type of the fallback
   * @param flagKey the flag
   * @param context a context map, see {@link Context}
   * @param fallback what to return when nothing usable is served
   * @return the answer
   */
  public <T> Detail<T> variationDetail(String flagKey, Object context, T fallback) {
    return answer(flagKey, context, fallback, Client::generic);
  }

  /**
   * A boolean flag's value.
   *
   * @param flagKey the flag
   * @param context a context map
   * @param fallback what to return when nothing usable is served
   * @return the served value or the fallback
   */
  public boolean boolVariation(String flagKey, Object context, boolean fallback) {
    return boolVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #boolVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context a context map
   * @param fallback what to return when nothing usable is served
   * @return the answer
   */
  public Detail<Boolean> boolVariationDetail(String flagKey, Object context, boolean fallback) {
    return answer(
        flagKey, context, fallback, (served, f) -> served instanceof Boolean ? served : MISMATCH);
  }

  /**
   * A string flag's value.
   *
   * @param flagKey the flag
   * @param context a context map
   * @param fallback what to return when nothing usable is served
   * @return the served value or the fallback
   */
  public String stringVariation(String flagKey, Object context, String fallback) {
    return stringVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #stringVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context a context map
   * @param fallback what to return when nothing usable is served
   * @return the answer
   */
  public Detail<String> stringVariationDetail(String flagKey, Object context, String fallback) {
    return answer(
        flagKey, context, fallback, (served, f) -> served instanceof String ? served : MISMATCH);
  }

  /**
   * A number flag's value.
   *
   * @param flagKey the flag
   * @param context a context map
   * @param fallback what to return when nothing usable is served
   * @return the served value or the fallback
   */
  public double numberVariation(String flagKey, Object context, double fallback) {
    return numberVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #numberVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context a context map
   * @param fallback what to return when nothing usable is served
   * @return the answer
   */
  public Detail<Double> numberVariationDetail(String flagKey, Object context, double fallback) {
    return answer(
        flagKey,
        context,
        fallback,
        (served, f) -> served instanceof Number n ? n.doubleValue() : MISMATCH);
  }

  /**
   * An integer flag's value. A served number with a fractional part, or outside the range of {@code
   * long}, is a {@code TYPE_MISMATCH}; {@code 2.0} counts as {@code 2}.
   *
   * @param flagKey the flag
   * @param context a context map
   * @param fallback what to return when nothing usable is served
   * @return the served value or the fallback
   */
  public long intVariation(String flagKey, Object context, long fallback) {
    return intVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Like {@link #intVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context a context map
   * @param fallback what to return when nothing usable is served
   * @return the answer
   */
  public Detail<Long> intVariationDetail(String flagKey, Object context, long fallback) {
    return answer(flagKey, context, fallback, (served, f) -> integral(served));
  }

  /** The exact {@code Long} of a served number, or {@link #MISMATCH}. */
  private static Object integral(Object served) {
    if (!(served instanceof Number n)) {
      return MISMATCH;
    }
    double d = n.doubleValue();
    if (d == Math.rint(d) && d >= -0x1p63 && d < 0x1p63) {
      return (long) d;
    }
    return MISMATCH;
  }

  /** The generic accessor: same JSON kind as the fallback, a null fallback accepts any. */
  private static Object generic(Object served, Object fallback) {
    if (fallback == null) {
      return served;
    }
    if (served == null) {
      return MISMATCH;
    }
    if (fallback instanceof Boolean) {
      return served instanceof Boolean ? served : MISMATCH;
    }
    if (fallback instanceof String) {
      return served instanceof String ? served : MISMATCH;
    }
    if (fallback instanceof List) {
      return served instanceof List ? served : MISMATCH;
    }
    if (fallback instanceof Map) {
      return served instanceof Map ? served : MISMATCH;
    }
    if (fallback instanceof Number && served instanceof Number n) {
      return numberLike(n.doubleValue(), fallback);
    }
    return MISMATCH;
  }

  private static Object numberLike(double d, Object fallback) {
    if (fallback instanceof Double) {
      return d;
    }
    if (fallback instanceof Float) {
      return (float) d;
    }
    Object whole = integral(Double.valueOf(d));
    if (whole == MISMATCH) {
      return MISMATCH;
    }
    long value = (Long) whole;
    if (fallback instanceof Long) {
      return value;
    }
    if (fallback instanceof Integer && value >= Integer.MIN_VALUE && value <= Integer.MAX_VALUE) {
      return (int) value;
    }
    return MISMATCH;
  }

  @SuppressWarnings("unchecked")
  private <T> Detail<T> answer(String flagKey, Object context, T fallback, Converter converter) {
    Sync.View view = sync.view();
    Detail<T> detail;
    try {
      detail = decide(view, flagKey, context, fallback, converter);
    } catch (RuntimeException | StackOverflowError failure) {
      onError.accept(failure);
      detail = new Detail<>(fallback, null, Reason.error(ErrorCode.EVALUATION_FAILED));
    }
    Consumer<EvaluationEvent> hook = options.afterEvaluation();
    if (hook != null) {
      runHook(hook, view, flagKey, context, detail);
    }
    return detail;
  }

  @SuppressWarnings("unchecked")
  private <T> Detail<T> decide(
      Sync.View view, String flagKey, Object context, T fallback, Converter converter) {
    Ruleset ruleset = view.ruleset();
    if (ruleset == null) {
      return new Detail<>(fallback, null, Reason.error(ErrorCode.NOT_READY));
    }
    Result result = options.evaluation().evaluate(ruleset, flagKey, context);
    Integer index = result.variationIndex();
    if (index == null) {
      return new Detail<>(fallback, null, result.reason());
    }
    Object converted = converter.convert(result.value(), fallback);
    if (converted == MISMATCH) {
      return new Detail<>(fallback, null, Reason.error(ErrorCode.TYPE_MISMATCH));
    }
    if (!closing) {
      usage.record(flagKey, index);
    }
    return new Detail<>((T) converted, index, result.reason());
  }

  private void runHook(
      Consumer<EvaluationEvent> hook,
      Sync.View view,
      String flagKey,
      Object context,
      Detail<?> detail) {
    try {
      Ruleset ruleset = view.ruleset();
      Long version = ruleset == null ? null : ruleset.flagVersion(flagKey);
      hook.accept(
          new EvaluationEvent(
              flagKey,
              context,
              detail.value(),
              detail.variationIndex(),
              detail.reason(),
              view.etag(),
              version));
    } catch (RuntimeException | StackOverflowError failure) {
      onError.accept(failure);
    }
  }

  /**
   * What this client has loaded and how its stream is doing: one snapshot, from memory, with no
   * network call.
   *
   * @return the status
   */
  public Status status() {
    return sync.status();
  }

  /**
   * Sends the pending usage counts now. A flush during another waits for it; counts held back by a
   * {@code Retry-After} stay pending. Never throws.
   */
  public void flush() {
    try {
      usage.flush();
    } catch (RuntimeException | StackOverflowError failure) {
      onError.accept(failure);
    }
  }

  /**
   * Stops following the server, ends the loops and sends the pending usage counts. Safe to call
   * twice and from many threads: every call waits for the same shutdown (a call on one of the SDK's
   * own threads does not wait). Evaluation still answers afterwards, and {@link #status()} reads
   * {@code closed}. It returns within about twice the request timeout.
   */
  @Override
  public void close() {
    boolean onSdkThread = Thread.currentThread() instanceof SdkThread;
    if (!closeStarted.compareAndSet(false, true)) {
      if (!onSdkThread) {
        awaitShutdown();
      }
      return;
    }
    try {
      shutDown();
    } finally {
      closeDone.countDown();
    }
  }

  private void awaitShutdown() {
    boolean interrupted = false;
    for (; ; ) {
      try {
        closeDone.await();
        break;
      } catch (InterruptedException e) {
        interrupted = true;
      }
    }
    if (interrupted) {
      Thread.currentThread().interrupt();
    }
  }

  private void shutDown() {
    closing = true;
    sync.stop();
    flushWake.wakeAll();
    join(syncThread);
    join(flushThread);
    settle();
    try {
      usage.finish();
    } catch (RuntimeException | StackOverflowError failure) {
      onError.accept(failure);
    }
  }

  private void join(Thread thread) {
    if (thread == null || thread == Thread.currentThread()) {
      return;
    }
    try {
      thread.join(Math.max(1, TimeUnit.NANOSECONDS.toMillis(options.requestNanos())));
    } catch (InterruptedException interrupted) {
      Thread.currentThread().interrupt();
    }
  }

  /** Test seam: how long a {@code Retry-After} still holds the ruleset and the stream. */
  long rulesetHoldNanos() {
    return sync.holdRemainingNanos();
  }

  /** Test seam: how long a {@code Retry-After} still holds the usage requests. */
  long usageHoldNanos() {
    return usage.holdRemainingNanos();
  }

  /** Test seam: how many usage entries wait to be sent. */
  int pendingUsage() {
    return usage.pendingEntries();
  }
}
