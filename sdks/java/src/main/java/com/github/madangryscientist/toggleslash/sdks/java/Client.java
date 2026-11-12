package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.eval.Evaluation;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Evaluator;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.internal.Core;
import com.github.madangryscientist.toggleslash.sdks.java.internal.Hub;
import com.github.madangryscientist.toggleslash.sdks.java.internal.SdkException;
import com.github.madangryscientist.toggleslash.sdks.java.internal.SdkThread;
import com.github.madangryscientist.toggleslash.sdks.java.internal.Sync;
import com.github.madangryscientist.toggleslash.sdks.java.internal.Tuning;
import com.github.madangryscientist.toggleslash.sdks.java.internal.Usage;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Future;

/**
 * A ToggleSlash server SDK client: it holds the ruleset of one environment, keeps it current, and
 * answers flag evaluations from memory. Nothing it does throws at a caller, and every method is
 * safe to call from many threads.
 */
public final class Client implements AutoCloseable {
  private final Hub hub;
  private final Sync sync;
  private final Usage usage;
  private final CountDownLatch done = new CountDownLatch(1);

  private Client(Options options, Tuning tuning) {
    this.hub = new Hub(options, tuning);
    this.sync = new Sync(hub);
    this.usage = new Usage(hub);
    this.sync.start();
    this.usage.start();
  }

  /**
   * Creates a client and starts loading the ruleset. It returns at once and never throws: a bad URL
   * or key shows up as a {@code network} error in {@link #status()} and {@code onError}.
   *
   * @param options the options
   * @return the client
   */
  public static Client create(Options options) {
    return create(options, null);
  }

  static Client create(Options options, Tuning tuning) {
    try {
      return new Client(options, tuning);
    } catch (Throwable e) {
      return new Client(Options.builder().url("http://127.0.0.1:9").sdkKey("-").build(), null);
    }
  }

  /**
   * Waits for the first load to finish.
   *
   * @return true if a ruleset is held; the answer is fixed at the first settle
   */
  public boolean ready() {
    try {
      if (Thread.currentThread() instanceof SdkThread) {
        return hub.readyNow();
      }
      return hub.awaitReady();
    } catch (Throwable e) {
      return false;
    }
  }

  /**
   * Reads the SDK's state: one consistent snapshot, no network.
   *
   * @return the status
   */
  public Status status() {
    Core core = hub.core();
    return new Status(core.etag(), core.lastSyncAt(), core.stream(), core.lastError());
  }

  /**
   * Stops the SDK: abandons what is in flight, sends the last usage counts and returns. It is
   * idempotent and safe from any thread; on an SDK thread it does not wait for itself.
   */
  @Override
  public void close() {
    try {
      boolean first = hub.beginClose();
      hub.settleReady(true);
      boolean inside = Thread.currentThread() instanceof SdkThread;
      if (first) {
        if (inside) {
          new SdkThread("toggleslash-close", this::shutdown).start();
        } else {
          shutdown();
        }
      }
      if (!inside) {
        done.await();
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    } catch (Throwable e) {
      // close never throws
    }
  }

  private void shutdown() {
    try {
      long bound = hub.requestTimeout().toMillis();
      sync.join(bound);
      usage.join(bound);
      usage.flush(true);
      sync.release();
    } catch (Throwable e) {
      // the shutdown ends either way
    } finally {
      done.countDown();
    }
  }

  /**
   * Evaluates a boolean flag.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served
   * @return the value or the fallback
   */
  public boolean boolVariation(String flagKey, Object context, boolean fallback) {
    return boolVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Evaluates a boolean flag with the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served
   * @return the value, variation index and reason
   */
  public Detail<Boolean> boolVariationDetail(String flagKey, Object context, boolean fallback) {
    Detail<Object> d = evaluate(flagKey, context, Kinds.Want.BOOL, fallback, Boolean.class);
    return new Detail<>((Boolean) d.value(), d.variationIndex(), d.reason());
  }

  /**
   * Evaluates a string flag.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served, may be null
   * @return the value or the fallback
   */
  public String stringVariation(String flagKey, Object context, String fallback) {
    return stringVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Evaluates a string flag with the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served, may be null
   * @return the value, variation index and reason
   */
  public Detail<String> stringVariationDetail(String flagKey, Object context, String fallback) {
    Detail<Object> d = evaluate(flagKey, context, Kinds.Want.STRING, fallback, String.class);
    return new Detail<>((String) d.value(), d.variationIndex(), d.reason());
  }

  /**
   * Evaluates an integer flag: the served number must have no fraction and fit a signed 64-bit
   * integer.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served
   * @return the value or the fallback
   */
  public long intVariation(String flagKey, Object context, long fallback) {
    return intVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Evaluates an integer flag with the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served
   * @return the value, variation index and reason
   */
  public Detail<Long> intVariationDetail(String flagKey, Object context, long fallback) {
    Detail<Object> d = evaluate(flagKey, context, Kinds.Want.INT, fallback, Long.class);
    return new Detail<>((Long) d.value(), d.variationIndex(), d.reason());
  }

  /**
   * Evaluates a number flag.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served
   * @return the value or the fallback
   */
  public double floatVariation(String flagKey, Object context, double fallback) {
    return floatVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Evaluates a number flag with the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served
   * @return the value, variation index and reason
   */
  public Detail<Double> floatVariationDetail(String flagKey, Object context, double fallback) {
    Detail<Object> d = evaluate(flagKey, context, Kinds.Want.FLOAT, fallback, Double.class);
    return new Detail<>((Double) d.value(), d.variationIndex(), d.reason());
  }

  /**
   * Evaluates a JSON flag. The tree is read-only: {@code List}, {@code Map}, {@code String}, {@code
   * Double}, {@code Boolean} and null. A null fallback accepts any kind; another fallback accepts
   * the kind it has (array, object, string, number, boolean).
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served
   * @return the value or the fallback
   */
  public Object jsonVariation(String flagKey, Object context, Object fallback) {
    return jsonVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * Evaluates a JSON flag with the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served
   * @return the value, variation index and reason
   */
  public Detail<Object> jsonVariationDetail(String flagKey, Object context, Object fallback) {
    return evaluate(flagKey, context, Kinds.Want.JSON, fallback, null);
  }

  /**
   * Evaluates a flag by the class of the fallback: {@code Boolean}, {@code String}, an integer
   * class ({@code Byte}, {@code Short}, {@code Integer}, {@code Long}), {@code Float}, {@code
   * Double}, {@code List} or {@code Map}. A null fallback accepts any kind; a class with no JSON
   * kind gives the fallback and {@code TYPE_MISMATCH}.
   *
   * @param <T> the fallback's type
   * @param flagKey the flag
   * @param context the context
   * @param fallback the value to return when the flag cannot be served
   * @return the value or the fallback
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
   * @param fallback the value to return when the flag cannot be served
   * @return the value, variation index and reason
   */
  @SuppressWarnings("unchecked")
  public <T> Detail<T> variationDetail(String flagKey, Object context, T fallback) {
    Detail<Object> d =
        evaluate(
            flagKey,
            context,
            Kinds.wantOf(fallback),
            fallback,
            fallback == null ? null : fallback.getClass());
    return new Detail<>((T) d.value(), d.variationIndex(), d.reason());
  }

  private Detail<Object> evaluate(
      String flagKey, Object context, Kinds.Want want, Object fallback, Class<?> type) {
    Core core = hub.core();
    Object value = fallback;
    Integer index = null;
    Reason reason;
    boolean served = false;
    Long version = null;
    try {
      Ruleset ruleset = core.ruleset();
      if (ruleset == null) {
        reason = Reason.error(ErrorCode.NOT_READY);
      } else {
        version = ruleset.flagVersion(flagKey);
        Evaluation result = Evaluator.evaluate(ruleset, flagKey, context);
        if (result.variationIndex() == null) {
          reason = result.reason();
        } else {
          Object converted = Kinds.convert(want, result.value(), fallback, type);
          if (converted == Kinds.MISMATCH) {
            reason = Reason.error(ErrorCode.TYPE_MISMATCH);
          } else {
            value = converted;
            index = result.variationIndex();
            reason = result.reason();
            served = true;
          }
        }
      }
    } catch (Throwable e) {
      value = fallback;
      index = null;
      served = false;
      reason = Reason.error(ErrorCode.EVALUATION_FAILED);
      hub.report(new SdkException("evaluation failed: " + e.getClass().getSimpleName(), e));
    }
    if (served) {
      usage.count(flagKey, index);
    }
    runHook(flagKey, context, value, index, reason, core.etag(), version);
    return new Detail<>(value, index, reason);
  }

  private void runHook(
      String flagKey,
      Object context,
      Object value,
      Integer index,
      Reason reason,
      String etag,
      Long version) {
    Hook hook = hub.options().afterEvaluation();
    if (hook == null) {
      return;
    }
    try {
      Object result =
          hook.onEvaluation(
              new EvaluationEvent(flagKey, context, value, index, reason, etag, version));
      watch(result);
    } catch (Throwable e) {
      hub.report(e);
    }
  }

  private void watch(Object result) throws InterruptedException {
    if (result instanceof CompletionStage<?> stage) {
      stage.whenComplete((v, error) -> {
        if (error != null) {
          hub.report(error);
        }
      });
    } else if (result instanceof Future<?> future && future.isDone()) {
      try {
        future.get();
      } catch (ExecutionException e) {
        hub.report(e.getCause() == null ? e : e.getCause());
      } catch (RuntimeException e) {
        hub.report(e);
      }
    }
  }
}
