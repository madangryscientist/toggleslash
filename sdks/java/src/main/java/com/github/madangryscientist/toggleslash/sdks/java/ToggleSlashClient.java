package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.EvaluationResult;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Evaluator;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ReasonKind;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.io.IOException;
import java.io.InputStream;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.OptionalInt;
import java.util.OptionalLong;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;

/**
 * The ToggleSlash server SDK for Java: it keeps a ruleset from a ToggleSlash server (polling, or
 * following the server's stream) and evaluates flags against it in process, so an evaluation never
 * waits for the network. It meets {@code docs/sdk-contract.md}; the mapping of the contract's names
 * to this class is in {@code README.md}.
 *
 * <p>Nothing here throws at a caller: a failure becomes the fallback with an {@code ERROR} reason,
 * or goes to {@code onError}, or both. Every method is safe to call from many threads.
 *
 * <p>The client runs two daemon threads (the sync loop and the usage flush), so one that is never
 * closed does not keep the JVM alive. {@link #close} ends them and sends the pending usage counts.
 */
public final class ToggleSlashClient implements AutoCloseable {

  private static final long NO_HOLD = Long.MIN_VALUE;
  private static final int BATCH = 500;

  /** The immutable view {@code status} and every evaluation read in one step. */
  private record State(
      Ruleset ruleset, String etag, Long lastSyncMillis, StreamState stream, String lastError) {

    State withStream(StreamState value) {
      return stream == StreamState.CLOSED
          ? this
          : new State(ruleset, etag, lastSyncMillis, value, lastError);
    }

    State withError(String code) {
      return new State(ruleset, etag, lastSyncMillis, stream, code);
    }

    State synced(long nowMillis) {
      return new State(ruleset, etag, nowMillis, stream, null);
    }

    State loaded(Ruleset value, String tag, long nowMillis) {
      return new State(value, tag, nowMillis, stream, null);
    }

    State connected() {
      return stream == StreamState.CLOSED
          ? this
          : new State(ruleset, etag, lastSyncMillis, StreamState.CONNECTED, null);
    }
  }

  /** What one ruleset attempt came to; {@code error} is null on success and when ignored. */
  private record Attempt(boolean ok, ToggleSlashException error) {
    static final Attempt OK = new Attempt(true, null);
    static final Attempt IGNORED = new Attempt(false, null);
  }

  private enum Kind {
    BOOL,
    STRING,
    INT,
    DOUBLE,
    JSON
  }

  private final ClientOptions options;
  private final Clock clock;
  private final Transport transport;
  private final DoubleSupplier jitter;
  private final AtomicReference<State> state;
  private final UsageCounts usage = new UsageCounts();
  private final AtomicLong rulesetHold = new AtomicLong(NO_HOLD);
  private final AtomicLong usageHold = new AtomicLong(NO_HOLD);
  private final Sleeper syncSleeper = new Sleeper();
  private final Sleeper flushSleeper = new Sleeper();
  private final CompletableFuture<Boolean> ready = new CompletableFuture<>();
  private final CompletableFuture<Void> done = new CompletableFuture<>();
  private final AtomicBoolean closing = new AtomicBoolean();
  private final Object gate = new Object();
  private boolean flushing;
  private ScheduledExecutorService timer;
  private Thread syncThread;
  private Thread flushThread;
  private volatile Thread closer;

  private ToggleSlashClient(ClientOptions options) {
    this.options = options;
    this.clock = options.clock();
    this.jitter = options.jitter();
    this.transport = new Transport(options);
    this.state =
        new AtomicReference<>(
            new State(
                null,
                null,
                null,
                options.stream() ? StreamState.CONNECTING : StreamState.OFF,
                null));
  }

  /**
   * Creates a client and starts it. It returns at once and never waits for the network: the first
   * ruleset request runs on the client's own thread, and {@link #ready} settles when it ends. Until
   * a ruleset is held every evaluation is the caller's fallback with {@code NOT_READY}.
   */
  public static ToggleSlashClient create(ClientOptions options) {
    ClientOptions use = options == null ? ClientOptions.builder("", "").build() : options;
    ToggleSlashClient client = new ToggleSlashClient(use);
    client.start();
    return client;
  }

  private void start() {
    syncThread = new Thread(this::syncMain, "toggleslash-sync");
    syncThread.setDaemon(true);
    flushThread = new Thread(this::flushMain, "toggleslash-flush");
    flushThread.setDaemon(true);
    syncThread.start();
    flushThread.start();
  }

  // ---------------------------------------------------------------- operations

  /**
   * Blocks until the first ruleset attempt ends (bounded by {@code requestTimeout}) and answers
   * whether a ruleset is held. The answer is fixed when that attempt settles, so asking again gives
   * the same one; after {@link #close} it is false if the attempt had not settled. Called from one
   * of the client's own threads (inside {@code onError} or a hook on it) it returns at once with
   * what is held.
   */
  public boolean ready() {
    try {
      if (!ready.isDone() && onSdkThread()) {
        return state.get().ruleset() != null;
      }
      return ready.get();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return state.get().ruleset() != null;
    } catch (ExecutionException | RuntimeException e) {
      return false;
    }
  }

  /** The boolean flag's value for {@code context}, or {@code fallback}. */
  public boolean boolVariation(String flagKey, Object context, boolean fallback) {
    return (Boolean) evaluate(flagKey, context, fallback, Kind.BOOL).value();
  }

  /** The boolean flag's value, its variation index and the reason. */
  public Evaluation<Boolean> boolVariationDetail(String flagKey, Object context, boolean fallback) {
    return typed(evaluate(flagKey, context, fallback, Kind.BOOL));
  }

  /** The string flag's value for {@code context}, or {@code fallback} (which may be null). */
  public String stringVariation(String flagKey, Object context, String fallback) {
    return (String) evaluate(flagKey, context, fallback, Kind.STRING).value();
  }

  /** The string flag's value, its variation index and the reason. */
  public Evaluation<String> stringVariationDetail(String flagKey, Object context, String fallback) {
    return typed(evaluate(flagKey, context, fallback, Kind.STRING));
  }

  /**
   * The integer flag's value, or {@code fallback}. A served number with a fractional part, or
   * outside the signed 64-bit range, is {@code TYPE_MISMATCH}; {@code 2.0} is {@code 2}.
   */
  public long intVariation(String flagKey, Object context, long fallback) {
    return (Long) evaluate(flagKey, context, fallback, Kind.INT).value();
  }

  /** The integer flag's value, its variation index and the reason. */
  public Evaluation<Long> intVariationDetail(String flagKey, Object context, long fallback) {
    return typed(evaluate(flagKey, context, fallback, Kind.INT));
  }

  /** The number flag's value as a double, or {@code fallback}. */
  public double doubleVariation(String flagKey, Object context, double fallback) {
    return (Double) evaluate(flagKey, context, fallback, Kind.DOUBLE).value();
  }

  /** The number flag's value, its variation index and the reason. */
  public Evaluation<Double> doubleVariationDetail(String flagKey, Object context, double fallback) {
    return typed(evaluate(flagKey, context, fallback, Kind.DOUBLE));
  }

  /**
   * The flag's value as any JSON value: {@code Boolean}, {@code Double}, {@code String}, {@code
   * List} or {@code Map}. A null {@code fallback} accepts any kind; otherwise the served value must
   * be the kind of the fallback.
   */
  public Object jsonVariation(String flagKey, Object context, Object fallback) {
    return evaluate(flagKey, context, fallback, Kind.JSON).value();
  }

  /** The generic flag's value, its variation index and the reason. */
  public Evaluation<Object> jsonVariationDetail(String flagKey, Object context, Object fallback) {
    return evaluate(flagKey, context, fallback, Kind.JSON);
  }

  /** One consistent snapshot of what the client knows; it reads memory and never fails. */
  public Status status() {
    State s = state.get();
    return new Status(
        Optional.ofNullable(s.etag()),
        s.lastSyncMillis() == null
            ? Optional.empty()
            : Optional.of(Instant.ofEpochMilli(s.lastSyncMillis())),
        s.stream(),
        Optional.ofNullable(s.lastError()));
  }

  /**
   * Stops the client: no more ruleset or stream requests, then the usage flush loop, then one last
   * flush of the pending counts (which does not wait out a {@code Retry-After}). Safe to call twice
   * or from many threads: every call returns when the shutdown is done, within about twice {@code
   * requestTimeout}. Called from one of the client's own threads it stops the rest and returns
   * without waiting for itself. Evaluation still answers afterwards, from the last ruleset, and
   * counts nothing.
   */
  @Override
  public void close() {
    try {
      if (closing.compareAndSet(false, true)) {
        stop();
        if (onSdkThread()) {
          Thread drainer =
              new Thread(
                  () -> {
                    closer = Thread.currentThread();
                    drain();
                  },
                  "toggleslash-close");
          drainer.setDaemon(true);
          drainer.start();
        } else {
          closer = Thread.currentThread();
          drain();
        }
      } else if (!onSdkThread()) {
        done.get(options.requestTimeout().toNanos() * 4 + 5_000_000_000L, TimeUnit.NANOSECONDS);
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    } catch (ExecutionException | TimeoutException | RuntimeException e) {
      // close never fails; a late shutdown is already as done as it will be
    }
  }

  // ---------------------------------------------------------------- evaluation

  @SuppressWarnings("unchecked")
  private static <T> Evaluation<T> typed(Evaluation<Object> evaluation) {
    return (Evaluation<T>) evaluation;
  }

  private Evaluation<Object> evaluate(String flagKey, Object context, Object fallback, Kind kind) {
    State held = state.get();
    Evaluation<Object> result;
    OptionalLong version = OptionalLong.empty();
    try {
      Ruleset ruleset = held.ruleset();
      if (ruleset == null) {
        result = fallbackWith(fallback, Reason.error(ErrorCode.NOT_READY));
      } else {
        EvaluationResult evaluated = Evaluator.evaluate(ruleset, flagKey, context);
        version = ruleset.flagVersion(flagKey);
        if (evaluated.reason().kind() == ReasonKind.ERROR || evaluated.variationIndex().isEmpty()) {
          result = fallbackWith(fallback, evaluated.reason());
        } else {
          Object served = accept(kind, evaluated.value(), fallback);
          if (served == MISMATCH) {
            result = fallbackWith(fallback, Reason.error(ErrorCode.TYPE_MISMATCH));
          } else {
            result = new Evaluation<>(served, evaluated.variationIndex(), evaluated.reason());
            count(flagKey, evaluated.variationIndex().getAsInt());
          }
        }
      }
    } catch (RuntimeException | StackOverflowError e) {
      result = fallbackWith(fallback, Reason.error(ErrorCode.EVALUATION_FAILED));
      version = OptionalLong.empty();
      report(e);
    }
    callHook(flagKey, context, result, held.etag(), version);
    return result;
  }

  private static Evaluation<Object> fallbackWith(Object fallback, Reason reason) {
    return new Evaluation<>(fallback, OptionalInt.empty(), reason);
  }

  private void count(String flagKey, int variationIndex) {
    if (!closing.get()) {
      usage.add(flagKey, variationIndex, clock.wallMillis());
    }
  }

  private void callHook(
      String flagKey,
      Object context,
      Evaluation<Object> result,
      String etag,
      OptionalLong flagVersion) {
    Consumer<EvaluationEvent> hook = options.afterEvaluation();
    if (hook == null) {
      return;
    }
    try {
      hook.accept(
          new EvaluationEvent(
              flagKey,
              context,
              result.value(),
              result.variationIndex(),
              result.reason(),
              Optional.ofNullable(etag),
              flagVersion));
    } catch (Throwable e) {
      report(e);
    }
  }

  /** Marks "the served value is not the caller's kind". */
  private static final Object MISMATCH = new Object();

  private static final double TWO_63 = 9.223372036854775808E18;

  /** The value to return when the served value fits the accessor's kind, else {@link #MISMATCH}. */
  private static Object accept(Kind kind, Object served, Object fallback) {
    switch (kind) {
      case BOOL:
        return served instanceof Boolean ? served : MISMATCH;
      case STRING:
        return served instanceof String || (served == null && fallback == null) ? served : MISMATCH;
      case DOUBLE:
        return served instanceof Number n ? (Object) n.doubleValue() : MISMATCH;
      case INT:
        if (served instanceof Number n) {
          double d = n.doubleValue();
          if (d == Math.rint(d) && d >= -TWO_63 && d < TWO_63) {
            return (long) d;
          }
        }
        return MISMATCH;
      default:
        return sameJsonKind(served, fallback) ? served : MISMATCH;
    }
  }

  /** A null fallback accepts any served value; a fallback that is no JSON kind does too. */
  private static boolean sameJsonKind(Object served, Object fallback) {
    if (fallback == null) {
      return true;
    }
    String want = jsonKind(fallback);
    return want == null || want.equals(jsonKind(served));
  }

  private static String jsonKind(Object value) {
    if (value instanceof Boolean) {
      return "boolean";
    } else if (value instanceof Number) {
      return "number";
    } else if (value instanceof String) {
      return "string";
    } else if (value instanceof List) {
      return "array";
    } else if (value instanceof Map) {
      return "object";
    }
    return null;
  }

  // ---------------------------------------------------------------- callbacks

  private boolean onSdkThread() {
    Thread current = Thread.currentThread();
    return current == syncThread || current == flushThread || current == closer;
  }

  /** Hands a failure to {@code onError}; whatever it raises is ignored. */
  private void report(Throwable error) {
    Consumer<Throwable> sink = options.onError();
    if (sink == null || error == null) {
      return;
    }
    try {
      sink.accept(error);
    } catch (Throwable ignored) {
      // an onError that raises is ignored (contract 10)
    }
  }

  private void report(Attempt attempt) {
    report(attempt.error());
  }

  // ---------------------------------------------------------------- the sync loop

  private void syncMain() {
    Attempt first = Attempt.IGNORED;
    try {
      first = fetchRuleset();
    } catch (RuntimeException | StackOverflowError e) {
      first = failure("ruleset", "network", e);
    } finally {
      ready.complete(state.get().ruleset() != null);
    }
    report(first);
    loop();
  }

  private void loop() {
    int failures = 0;
    while (!closing.get()) {
      boolean connected = false;
      try {
        if (options.stream()) {
          if (!sleepUntil(syncSleeper, rulesetHold.get())) {
            return;
          }
          connected = followStream();
        }
      } catch (RuntimeException | StackOverflowError e) {
        report(e);
      }
      failures = connected ? 0 : failures + 1;
      long wait = Math.max(clock.nanos() + delayNanos(failures), holdDeadline(rulesetHold));
      if (!sleepUntil(syncSleeper, wait)) {
        return;
      }
      if (!connected) {
        try {
          report(fetchRuleset());
        } catch (RuntimeException | StackOverflowError e) {
          report(e);
        }
      }
    }
  }

  private long delayNanos(int failures) {
    long poll = options.pollInterval().toNanos();
    if (!options.stream()) {
      return poll;
    }
    long ceiling = Math.min(poll, 1_000_000_000L << Math.min(failures, 16));
    return (long) (ceiling * jitter.getAsDouble());
  }

  /** The monotonic time a hold lasts until, or now when there is none. */
  private long holdDeadline(AtomicLong hold) {
    long until = hold.get();
    return until == NO_HOLD ? Long.MIN_VALUE : until;
  }

  /** Whether a hold is in force now. */
  private boolean held(AtomicLong hold) {
    long until = hold.get();
    return until != NO_HOLD && until - clock.nanos() > 0;
  }

  private void extendHold(AtomicLong hold, long seconds) {
    long until = clock.nanos() + seconds * 1_000_000_000L;
    hold.accumulateAndGet(until, (old, next) -> old == NO_HOLD || next - old > 0 ? next : old);
  }

  /**
   * Waits until {@code deadline} (a {@link Clock#nanos} time) or until {@code sleeper} is woken.
   * Returns false when it was woken, so the loop should end.
   */
  private boolean sleepUntil(Sleeper sleeper, long deadline) {
    try {
      while (true) {
        if (sleeper.isWoken()) {
          return false;
        }
        if (deadline == Long.MIN_VALUE || deadline == NO_HOLD) {
          return true;
        }
        long left = deadline - clock.nanos();
        if (left <= 0) {
          return true;
        }
        clock.await(sleeper, left);
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return false;
    }
  }

  // ---------------------------------------------------------------- the ruleset route

  private Attempt fetchRuleset() {
    State held = state.get();
    String ifNoneMatch =
        held.ruleset() != null && held.etag() != null && !held.etag().isEmpty()
            ? held.etag()
            : null;
    Transport.Reply reply = transport.getRuleset(ifNoneMatch);
    if (closing.get() || reply.fail() == Transport.Fail.CANCELLED) {
      return Attempt.IGNORED;
    }
    if (reply.fail() != Transport.Fail.NONE) {
      return failure(
          "ruleset", reply.fail() == Transport.Fail.TIMEOUT ? "timeout" : "network", reply.cause());
    }
    int status = reply.status();
    if (status == 200) {
      return load(reply);
    }
    if (status == 304 && state.get().ruleset() != null) {
      state.updateAndGet(s -> s.synced(clock.wallMillis()));
      return Attempt.OK;
    }
    if (status == 429) {
      RetryAfter.parse(reply.retryAfter()).ifPresent(seconds -> extendHold(rulesetHold, seconds));
    }
    return failure("ruleset", "http_" + status, null);
  }

  private Attempt load(Transport.Reply reply) {
    Ruleset ruleset;
    try {
      Object parsed = Json.parse(reply.body());
      if (!(parsed instanceof Map<?, ?> map)
          || !(map.get("flags") instanceof List)
          || !(map.get("segments") instanceof List)) {
        return failure("ruleset", "bad_ruleset", null);
      }
      ruleset = Ruleset.load(parsed);
    } catch (RuntimeException | StackOverflowError e) {
      return failure("ruleset", "bad_ruleset", e);
    }
    String etag = reply.etag() == null || reply.etag().isEmpty() ? null : reply.etag();
    state.updateAndGet(s -> s.loaded(ruleset, etag, clock.wallMillis()));
    return Attempt.OK;
  }

  private Attempt failure(String route, String code, Throwable cause) {
    state.updateAndGet(s -> s.withError(code));
    return new Attempt(false, new ToggleSlashException(route + ": " + code, code, cause));
  }

  // ---------------------------------------------------------------- the stream

  /** What one stream's events came to. Touched by the reading thread only. */
  private static final class Run {
    boolean connected;
    boolean refreshFailed;
    boolean stop;
  }

  /** The 60 s quiet-time watchdog: closes the stream when no byte arrives for a whole window. */
  private final class Watch implements Runnable {
    private final InputStream in;
    private final long window;
    private volatile long last = System.nanoTime();
    private volatile boolean timedOut;
    private volatile boolean stopped;
    private volatile ScheduledFuture<?> pending;

    Watch(InputStream in, long window) {
      this.in = in;
      this.window = window;
    }

    void start() {
      schedule(window);
    }

    void touch() {
      last = System.nanoTime();
    }

    void stop() {
      stopped = true;
      ScheduledFuture<?> p = pending;
      if (p != null) {
        p.cancel(false);
      }
    }

    private void schedule(long nanos) {
      ScheduledExecutorService t = timer();
      if (t != null && !stopped) {
        try {
          pending = t.schedule(this, nanos, TimeUnit.NANOSECONDS);
        } catch (RuntimeException ignored) {
          // the timer was shut down by close
        }
      }
    }

    @Override
    public void run() {
      if (stopped) {
        return;
      }
      long idle = System.nanoTime() - last;
      if (idle >= window) {
        timedOut = true;
        Transport.closeQuietly(in);
      } else {
        schedule(window - idle);
      }
    }
  }

  private synchronized ScheduledExecutorService timer() {
    if (closing.get()) {
      return null;
    }
    if (timer == null) {
      timer =
          Executors.newSingleThreadScheduledExecutor(runnable -> {
            Thread thread = new Thread(runnable, "toggleslash-timer");
            thread.setDaemon(true);
            return thread;
          });
    }
    return timer;
  }

  private void setStream(StreamState value) {
    state.updateAndGet(s -> s.withStream(value));
  }

  /** Follows the stream until it ends. Returns whether it connected and then stayed healthy. */
  private boolean followStream() {
    setStream(StreamState.CONNECTING);
    Transport.Opened opened = transport.openStream();
    if (closing.get() || opened.fail() == Transport.Fail.CANCELLED) {
      Transport.closeQuietly(opened.body());
      return false;
    }
    if (opened.fail() != Transport.Fail.NONE) {
      streamFailed(
          failure(
              "stream",
              opened.fail() == Transport.Fail.TIMEOUT ? "timeout" : "network",
              opened.cause()));
      return false;
    }
    if (opened.status() != 200) {
      Transport.closeQuietly(opened.body());
      if (opened.status() == 429) {
        RetryAfter.parse(opened.retryAfter())
            .ifPresent(seconds -> extendHold(rulesetHold, seconds));
      }
      streamFailed(failure("stream", "http_" + opened.status(), null));
      return false;
    }
    InputStream in = opened.body();
    Run run = new Run();
    Runnable abort = () -> Transport.closeQuietly(in);
    if (!transport.track(abort)) {
      Transport.closeQuietly(in);
      return false;
    }
    Watch watch = new Watch(in, options.watchdog().toNanos());
    watch.start();
    SseParser parser = new SseParser((name, data) -> onEvent(name, data, run));
    byte[] buffer = new byte[8192];
    IOException broken = null;
    try {
      int read;
      while (!run.stop && (read = in.read(buffer)) >= 0) {
        watch.touch();
        parser.feed(buffer, 0, read);
      }
    } catch (IOException e) {
      broken = e;
    } finally {
      watch.stop();
      transport.untrack(abort);
      Transport.closeQuietly(in);
    }
    if (closing.get()) {
      return false;
    }
    if (watch.timedOut) {
      streamFailed(failure("stream", "timeout", null));
    } else if (broken != null && !run.stop) {
      streamFailed(failure("stream", "network", broken));
    } else {
      setStream(StreamState.DISCONNECTED);
    }
    return run.connected && !run.refreshFailed;
  }

  private void streamFailed(Attempt attempt) {
    setStream(StreamState.DISCONNECTED);
    report(attempt);
  }

  private void onEvent(String name, String data, Run run) {
    if (run.stop) {
      return;
    }
    boolean isConnected = name.equals("connected");
    if (!isConnected && !name.equals("changed")) {
      return;
    }
    if (isConnected) {
      run.connected = true;
      state.updateAndGet(State::connected);
    }
    State held = state.get();
    if (held.ruleset() != null
        && held.etag() != null
        && !held.etag().isEmpty()
        && data.equals(held.etag())) {
      state.updateAndGet(s -> s.synced(clock.wallMillis()));
      return;
    }
    Attempt attempt = fetchRuleset();
    if (!attempt.ok()) {
      run.stop = true;
      run.refreshFailed = true;
      report(attempt);
    }
  }

  // ---------------------------------------------------------------- usage

  private void flushMain() {
    while (sleepUntil(flushSleeper, clock.nanos() + options.flushInterval().toNanos())) {
      try {
        flush(false);
      } catch (RuntimeException | StackOverflowError e) {
        report(e);
      }
    }
  }

  /** Runs one flush; one that starts while another runs waits for it. */
  private void flush(boolean last) {
    List<Throwable> errors = new ArrayList<>();
    synchronized (gate) {
      while (flushing) {
        try {
          gate.wait();
        } catch (InterruptedException e) {
          Thread.currentThread().interrupt();
          return;
        }
      }
      flushing = true;
    }
    try {
      flushNow(last, errors);
    } finally {
      synchronized (gate) {
        flushing = false;
        gate.notifyAll();
      }
    }
    for (Throwable error : errors) {
      report(error);
    }
  }

  private void flushNow(boolean last, List<Throwable> errors) {
    if (held(usageHold)) {
      int pending = usage.size();
      if (last && pending > 0) {
        errors.add(
            new ToggleSlashException(
                "closed with " + pending + " usage counts unsent: the server asked to wait",
                "unsent",
                null));
      }
      return;
    }
    List<UsageCounts.Entry> entries = usage.drain();
    int sent = 0;
    while (sent < entries.size()) {
      int end = Math.min(sent + BATCH, entries.size());
      Transport.Reply reply = transport.postEvents(UsageCounts.body(entries.subList(sent, end)));
      if (reply.fail() != Transport.Fail.NONE) {
        String code = reply.fail() == Transport.Fail.TIMEOUT ? "timeout" : "network";
        errors.add(new ToggleSlashException("usage: " + code, code, reply.cause()));
        break;
      }
      int status = reply.status();
      if (status >= 200 && status < 300) {
        sent = end;
        continue;
      }
      errors.add(new ToggleSlashException("usage: http_" + status, "http_" + status, null));
      if (status >= 500 || status == 408 || status == 429) {
        if (status == 429) {
          RetryAfter.parse(reply.retryAfter()).ifPresent(seconds -> extendHold(usageHold, seconds));
        }
        break;
      }
      sent = end;
    }
    if (sent < entries.size()) {
      usage.putBack(entries.subList(sent, entries.size()));
    }
  }

  // ---------------------------------------------------------------- close

  /**
   * The part of {@link #close} that never waits: after it no request is made and no state moves.
   */
  private void stop() {
    state.updateAndGet(s ->
        new State(s.ruleset(), s.etag(), s.lastSyncMillis(), StreamState.CLOSED, s.lastError()));
    ready.complete(false);
    transport.abortAll();
    syncSleeper.wake();
    flushSleeper.wake();
    ScheduledExecutorService t;
    synchronized (this) {
      t = timer;
    }
    if (t != null) {
      t.shutdownNow();
    }
  }

  /** The waiting part: the loops end, then the last flush goes out. */
  private void drain() {
    try {
      long deadline = System.nanoTime() + options.requestTimeout().toNanos();
      join(syncThread, deadline);
      join(flushThread, deadline);
      flush(true);
    } catch (RuntimeException | StackOverflowError e) {
      report(e);
    } finally {
      done.complete(null);
    }
  }

  private static void join(Thread thread, long deadlineNanos) {
    if (thread == null || thread == Thread.currentThread()) {
      return;
    }
    try {
      long left = deadlineNanos - System.nanoTime();
      if (left > 0) {
        thread.join(left / 1_000_000L + 1);
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }
}
