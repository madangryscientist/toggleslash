package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Result;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.OptionalInt;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.Consumer;

/**
 * A ToggleSlash server SDK client. It keeps a ruleset current in the background and evaluates flags
 * from it in memory. Every method is safe to call from many threads and none throws: a server that
 * is down, a bad answer or a bad context gives the fallback you passed and an {@code ERROR} reason.
 * Create one per process, and {@link #close()} it on the way out.
 */
public final class Client implements AutoCloseable {
  private final Options options;
  private final Consumer<Throwable> report;
  private final Http syncHttp;
  private final Hold syncHold;
  private final Hold usageHold;
  private final Sync sync;
  private final Usage usage;
  private final Thread syncThread;
  private final Thread flushThread;
  private final CountDownLatch flushStop = new CountDownLatch(1);
  private final CountDownLatch firstLoad = new CountDownLatch(1);
  private final AtomicBoolean firstLoadSettled = new AtomicBoolean();
  private volatile boolean firstLoadResult;
  private final AtomicBoolean closing = new AtomicBoolean();
  private final CountDownLatch closed = new CountDownLatch(1);
  private volatile Thread closer;

  private Client(Options options) {
    this.options = options;
    Consumer<Throwable> onError = options.onError();
    this.report =
        error -> {
          if (onError == null) {
            return;
          }
          try {
            onError.accept(error);
          } catch (RuntimeException | StackOverflowError ignored) {
            // A faulty error hook must not break the SDK.
          }
        };
    String baseUrl = stripSlashes(options.url());
    this.syncHttp = new Http(Http.newClient(options.requestTimeout()));
    Http usageHttp = new Http(Http.newClient(options.requestTimeout()));
    this.syncHold = new Hold(options.nanoTime());
    this.usageHold = new Hold(options.nanoTime());
    this.usage =
        new Usage(
            usageHttp,
            baseUrl,
            options.sdkKey(),
            options.requestTimeout(),
            options.clock(),
            usageHold,
            report);
    this.sync = new Sync(syncHttp, options, baseUrl, syncHold, report, this::settleFirstLoad);
    this.syncThread = daemon("toggleslash-sync", this::runSync);
    this.flushThread = daemon("toggleslash-flush", this::runFlush);
    syncThread.start();
    flushThread.start();
  }

  /**
   * Starts a client. It never throws and never waits: it returns at once and loads in the
   * background.
   */
  public static Client create(Options options) {
    return new Client(options);
  }

  private static String stripSlashes(String url) {
    if (url == null) {
      return "";
    }
    int end = url.length();
    while (end > 0 && url.charAt(end - 1) == '/') {
      end--;
    }
    return url.substring(0, end);
  }

  private static Thread daemon(String name, Runnable body) {
    Thread thread = new Thread(body, name);
    thread.setDaemon(true);
    return thread;
  }

  private void runSync() {
    try {
      sync.run();
    } catch (RuntimeException | Error e) {
      // The loop is bounded by design; if it ever fails, say so once and leave the ruleset as it
      // is.
      report.accept(e);
      settleFirstLoad();
    }
  }

  private void runFlush() {
    try {
      while (!flushStop.await(options.flushInterval().toNanos(), TimeUnit.NANOSECONDS)) {
        usage.flush();
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    } catch (RuntimeException | Error e) {
      report.accept(e);
    }
  }

  private void settleFirstLoad() {
    settle(sync.snapshot() != null);
  }

  private void settle(boolean result) {
    if (firstLoadSettled.compareAndSet(false, true)) {
      firstLoadResult = result;
      firstLoad.countDown();
    }
  }

  /**
   * Blocks until the first ruleset load has finished, and returns true if it worked. It never
   * throws and is bounded by the request timeout: false means flags serve fallbacks until the SDK
   * reaches the server. It may be called again and gives the same answer; after {@link #close()} it
   * is false if it had not settled.
   */
  public boolean ready() {
    boolean interrupted = false;
    while (true) {
      try {
        firstLoad.await();
        break;
      } catch (InterruptedException e) {
        interrupted = true;
      }
    }
    if (interrupted) {
      Thread.currentThread().interrupt();
    }
    return firstLoadResult;
  }

  /** What this SDK has loaded and how its stream is doing. Local, never waits, never throws. */
  public Status status() {
    return sync.status();
  }

  // ---- typed accessors ------------------------------------------------------------------------

  /** The flag's boolean for a context, or {@code fallback}. */
  public boolean boolVariation(String flagKey, Context context, boolean fallback) {
    return boolVariationDetail(flagKey, context, fallback).value();
  }

  /** Like {@link #boolVariation}, with the variation index and the reason. */
  public Detail<Boolean> boolVariationDetail(String flagKey, Context context, boolean fallback) {
    return cast(evaluate(flagKey, context, fallback, Kind.BOOL), Boolean.class);
  }

  /** The flag's string for a context, or {@code fallback} (which may be null). */
  public String stringVariation(String flagKey, Context context, String fallback) {
    return stringVariationDetail(flagKey, context, fallback).value();
  }

  /** Like {@link #stringVariation}, with the variation index and the reason. */
  public Detail<String> stringVariationDetail(String flagKey, Context context, String fallback) {
    return cast(evaluate(flagKey, context, fallback, Kind.STRING), String.class);
  }

  /** The flag's number for a context, or {@code fallback}. */
  public double doubleVariation(String flagKey, Context context, double fallback) {
    return doubleVariationDetail(flagKey, context, fallback).value();
  }

  /** Like {@link #doubleVariation}, with the variation index and the reason. */
  public Detail<Double> doubleVariationDetail(String flagKey, Context context, double fallback) {
    return cast(evaluate(flagKey, context, fallback, Kind.NUMBER), Double.class);
  }

  /**
   * The flag's integer for a context, or {@code fallback}. A served number with a fraction, or
   * outside the range of a {@code long}, is a {@code TYPE_MISMATCH}; {@code 2.0} is 2.
   */
  public long intVariation(String flagKey, Context context, long fallback) {
    return intVariationDetail(flagKey, context, fallback).value();
  }

  /** Like {@link #intVariation}, with the variation index and the reason. */
  public Detail<Long> intVariationDetail(String flagKey, Context context, long fallback) {
    Detail<Object> detail = evaluate(flagKey, context, fallback, Kind.INTEGER);
    Object value = detail.value();
    Long number = value instanceof Double d ? Long.valueOf(d.longValue()) : (Long) value;
    return new Detail<>(number, detail.variationIndex(), detail.reason());
  }

  /**
   * The flag's value as parsed JSON ({@code Map}, {@code List}, {@code String}, {@code Double},
   * {@code Boolean} or null), or {@code fallback}. The value must have the JSON kind of the
   * fallback; a null fallback accepts anything. Do not change what it returns: it is shared.
   */
  public Object jsonVariation(String flagKey, Context context, Object fallback) {
    return jsonVariationDetail(flagKey, context, fallback).value();
  }

  /** Like {@link #jsonVariation}, with the variation index and the reason. */
  public Detail<Object> jsonVariationDetail(String flagKey, Context context, Object fallback) {
    return evaluate(flagKey, context, fallback, Kind.OF_FALLBACK);
  }

  private static <T> Detail<T> cast(Detail<Object> detail, Class<T> type) {
    return new Detail<>(type.cast(detail.value()), detail.variationIndex(), detail.reason());
  }

  // ---- evaluation -----------------------------------------------------------------------------

  /** The JSON kind a served value must have for the accessor that asked. */
  private enum Kind {
    BOOL,
    STRING,
    NUMBER,
    INTEGER,
    OF_FALLBACK
  }

  private static boolean accepts(Kind kind, Object value, Object fallback) {
    if (value == null) {
      return fallback == null;
    }
    Kind wanted = kind;
    if (kind == Kind.OF_FALLBACK) {
      if (fallback == null) {
        return true;
      }
      if (fallback instanceof Boolean) {
        wanted = Kind.BOOL;
      } else if (fallback instanceof String) {
        wanted = Kind.STRING;
      } else if (fallback instanceof Number) {
        wanted = Kind.NUMBER;
      } else if (fallback instanceof Map) {
        return value instanceof Map;
      } else {
        return value instanceof List;
      }
    }
    switch (wanted) {
      case BOOL:
        return value instanceof Boolean;
      case STRING:
        return value instanceof String;
      case NUMBER:
        return value instanceof Double;
      case INTEGER:
        return value instanceof Double d
            && !d.isNaN()
            && !d.isInfinite()
            && d == Math.rint(d)
            && d >= -0x1p63
            && d < 0x1p63;
      default:
        return false;
    }
  }

  private Detail<Object> fallbackDetail(Object fallback, ErrorCode code) {
    return new Detail<>(fallback, OptionalInt.empty(), Reason.error(code));
  }

  private Detail<Object> evaluate(String flagKey, Context context, Object fallback, Kind kind) {
    Sync.Snapshot held = sync.snapshot();
    Detail<Object> detail;
    try {
      detail = compute(held, flagKey, context, fallback, kind);
    } catch (RuntimeException | StackOverflowError e) {
      report.accept(e);
      detail = fallbackDetail(fallback, ErrorCode.EVALUATION_FAILED);
    }
    Consumer<EvaluationEvent> hook = options.afterEvaluation();
    if (hook != null) {
      runHook(hook, held, flagKey, context, detail);
    }
    return detail;
  }

  private Detail<Object> compute(
      Sync.Snapshot held, String flagKey, Context context, Object fallback, Kind kind) {
    if (held == null) {
      return fallbackDetail(fallback, ErrorCode.NOT_READY);
    }
    Result result =
        options
            .evaluator()
            .evaluate(held.ruleset(), flagKey, context == null ? null : context.tree());
    if (result.variationIndex().isEmpty()) {
      return new Detail<>(fallback, OptionalInt.empty(), result.reason());
    }
    if (!accepts(kind, result.value(), fallback)) {
      return fallbackDetail(fallback, ErrorCode.TYPE_MISMATCH);
    }
    if (!closing.get()) {
      usage.record(flagKey, result.variationIndex().getAsInt());
    }
    return new Detail<>(result.value(), result.variationIndex(), result.reason());
  }

  private void runHook(
      Consumer<EvaluationEvent> hook,
      Sync.Snapshot held,
      String flagKey,
      Context context,
      Detail<Object> detail) {
    try {
      Optional<String> etag = Optional.ofNullable(held == null ? null : held.etag());
      Optional<Double> version =
          held == null ? Optional.empty() : held.ruleset().flagVersion(flagKey);
      hook.accept(
          new EvaluationEvent(
              flagKey,
              context,
              detail.value(),
              detail.variationIndex(),
              detail.reason(),
              etag,
              version));
    } catch (RuntimeException | StackOverflowError e) {
      report.accept(e);
    }
  }

  // ---- close ----------------------------------------------------------------------------------

  /**
   * Stops following the server and sends the pending usage counts. It never throws, is safe to call
   * twice or from many threads (each call returns once the shutdown has finished), and evaluation
   * still answers afterwards. Counts that could not be sent because the server asked to wait are
   * reported to {@code onError}.
   */
  @Override
  public void close() {
    Thread self = Thread.currentThread();
    if (closing.compareAndSet(false, true)) {
      closer = self;
      try {
        shutDown(self);
      } finally {
        closed.countDown();
      }
      return;
    }
    if (self == closer || self == syncThread || self == flushThread) {
      // A callback that closes the client from inside the shutdown, or from one of its own threads:
      // waiting here would wait for itself.
      return;
    }
    boolean interrupted = false;
    while (true) {
      try {
        closed.await();
        break;
      } catch (InterruptedException e) {
        interrupted = true;
      }
    }
    if (interrupted) {
      self.interrupt();
    }
  }

  private void shutDown(Thread self) {
    usage.stopRecording();
    sync.stop();
    flushStop.countDown();
    join(syncThread, self);
    join(flushThread, self);
    settle(false);
    try {
      usage.finish();
    } catch (RuntimeException | StackOverflowError e) {
      report.accept(e);
    }
  }

  private static void join(Thread thread, Thread self) {
    if (thread == self) {
      return;
    }
    boolean interrupted = false;
    while (thread.isAlive()) {
      try {
        thread.join();
      } catch (InterruptedException e) {
        interrupted = true;
      }
    }
    if (interrupted) {
      self.interrupt();
    }
  }

  // ---- test seams -----------------------------------------------------------------------------

  /** For the tests: the hold on the ruleset and stream routes. */
  Duration syncHoldRemaining() {
    return Duration.ofNanos(syncHold.remainingNanos());
  }

  /** For the tests: the hold on the events route. */
  Duration usageHoldRemaining() {
    return Duration.ofNanos(usageHold.remainingNanos());
  }

  /** For the tests: send the pending usage counts now. */
  void flushNow() {
    usage.flush();
  }
}
