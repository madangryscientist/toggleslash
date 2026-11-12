package com.github.madangryscientist.toggleslash.sdks.java;

import java.net.http.HttpRequest;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.locks.ReentrantLock;
import java.util.function.UnaryOperator;

/**
 * The ToggleSlash server SDK for Java. {@link #create} starts it; it loads the ruleset, follows the
 * server's stream (or polls), evaluates locally and reports usage counts. Nothing here throws at a
 * caller: a failure becomes the caller's fallback with an {@code ERROR} reason, or goes to {@code
 * onError}.
 *
 * <p>Every method is safe to call from any thread. Evaluation reads memory and never waits; the
 * SDK's two daemon threads (sync and flush) do all the I/O. The contract is {@code
 * docs/sdk-contract.md}.
 */
public final class ToggleSlashClient implements AutoCloseable {
  private static final long RETRY_AFTER_CAP_SECONDS = 300;
  private static final int MAX_ENTRIES_PER_REQUEST = 500;
  private static final int MAX_CONTEXT_DEPTH = 200;

  /** The ruleset and the tag that names it, swapped together. */
  private record Held(Ruleset ruleset, String etag) {}

  /** The kind of value an accessor wants. */
  private enum Want {
    ANY,
    BOOL,
    STRING,
    NUMBER,
    INT,
    ARRAY,
    OBJECT,
    OTHER
  }

  private static final class SdkThread extends Thread {
    final ToggleSlashClient owner;

    SdkThread(ToggleSlashClient owner, Runnable body, String name) {
      super(body, name);
      this.owner = owner;
      setDaemon(true);
    }
  }

  private record End(Throwable error) {}

  private final Options options;
  private final Http http;
  private final Usage usage = new Usage();

  private volatile Held held;
  private volatile Status status;
  private final Object stateLock = new Object();
  private volatile boolean closedFlag;

  private final CompletableFuture<Boolean> ready = new CompletableFuture<>();
  private final CountDownLatch closeLatch = new CountDownLatch(1);
  private final CountDownLatch doneLatch = new CountDownLatch(1);
  private final AtomicBoolean closeStarted = new AtomicBoolean();
  private volatile Thread closer;
  private final ReentrantLock flushLock = new ReentrantLock();

  private volatile long rulesetHoldUntil;
  private volatile long usageHoldUntil;

  private Thread syncThread;
  private Thread flushThread;

  private ToggleSlashClient(Options options) {
    this.options = options;
    String url = options.url();
    while (url.endsWith("/")) {
      url = url.substring(0, url.length() - 1);
    }
    this.http = new Http(url, options.sdkKey());
    this.status =
        new Status(null, null, options.stream() ? StreamState.CONNECTING : StreamState.OFF, null);
  }

  /**
   * Starts the SDK and returns at once; the first load runs on the SDK's own thread. It never
   * throws.
   *
   * @param options the options; null means the defaults with an empty URL (every request then fails
   *     with {@code network})
   * @return the client
   */
  public static ToggleSlashClient create(Options options) {
    Options given = options == null ? Options.builder("", "").build() : options;
    ToggleSlashClient client = new ToggleSlashClient(given);
    client.start();
    return client;
  }

  private void start() {
    syncThread = new SdkThread(this, this::syncMain, "toggleslash-sync");
    flushThread = new SdkThread(this, this::flushMain, "toggleslash-flush");
    try {
      syncThread.start();
      flushThread.start();
    } catch (RuntimeException | Error e) {
      reportError(e);
      ready.complete(false);
    }
  }

  // ------------------------------------------------------------------ public API

  /**
   * Waits for the first load to settle, which {@code requestTimeout} bounds. The answer is fixed
   * once settled: true if a ruleset was held then. Called on one of the SDK's own threads (from a
   * callback) it does not wait and answers from what is held.
   *
   * @return whether a ruleset has loaded
   */
  public boolean ready() {
    if (ready.isDone()) {
      return getReady();
    }
    if (onOwnThread()) {
      return held != null;
    }
    try {
      return ready.get(
          options.requestTimeout().multipliedBy(2).toMillis() + 1000, TimeUnit.MILLISECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return false;
    } catch (ExecutionException | TimeoutException e) {
      return held != null;
    }
  }

  private boolean getReady() {
    try {
      return ready.get();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return false;
    } catch (ExecutionException e) {
      return false;
    }
  }

  /**
   * A consistent snapshot of what the SDK has loaded. It never waits.
   *
   * @return the status
   */
  public Status status() {
    return status;
  }

  /**
   * Evaluates a boolean flag.
   *
   * @param flagKey the flag
   * @param context the context: a map with {@code key} (and optionally {@code kind}, {@code
   *     attributes}), or a multi-kind map
   * @param fallback what to return when the flag cannot be served as a boolean
   * @return the value or the fallback
   */
  public boolean boolVariation(String flagKey, Object context, boolean fallback) {
    return boolVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * As {@link #boolVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the detail
   */
  public Detail<Boolean> boolVariationDetail(String flagKey, Object context, boolean fallback) {
    return typed(flagKey, context, fallback, Want.BOOL);
  }

  /**
   * Evaluates a string flag.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback what to return when the flag cannot be served as a string (may be null)
   * @return the value or the fallback
   */
  public String stringVariation(String flagKey, Object context, String fallback) {
    return stringVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * As {@link #stringVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the detail
   */
  public Detail<String> stringVariationDetail(String flagKey, Object context, String fallback) {
    return typed(flagKey, context, fallback, Want.STRING);
  }

  /**
   * Evaluates a number flag as a double.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback what to return when the flag cannot be served as a number
   * @return the value or the fallback
   */
  public double doubleVariation(String flagKey, Object context, double fallback) {
    return doubleVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * As {@link #doubleVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the detail
   */
  public Detail<Double> doubleVariationDetail(String flagKey, Object context, double fallback) {
    return typed(flagKey, context, fallback, Want.NUMBER);
  }

  /**
   * Evaluates a number flag as a 64-bit integer. A served number with a fraction, or outside the
   * signed 64-bit range, is a {@code TYPE_MISMATCH} and the caller gets the fallback; {@code 2.0}
   * is the integer 2.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback what to return when the flag cannot be served as an integer
   * @return the value or the fallback
   */
  public long intVariation(String flagKey, Object context, long fallback) {
    return intVariationDetail(flagKey, context, fallback).value();
  }

  /**
   * As {@link #intVariation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the detail
   */
  public Detail<Long> intVariationDetail(String flagKey, Object context, long fallback) {
    return typed(flagKey, context, fallback, Want.INT);
  }

  /**
   * Evaluates a flag of any type; the kind to accept comes from the fallback: a {@link Boolean}, a
   * {@link Number}, a {@link CharSequence}, a {@link Collection} or array, a {@link Map}, or null
   * for any kind. Served values come back in the JSON model: {@code Boolean}, {@code Double},
   * {@code String}, {@code List} and {@code Map}. A fallback of another Java type matches nothing
   * served.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback, returned as given when the flag is not served
   * @return the value or the fallback
   */
  public Object variation(String flagKey, Object context, Object fallback) {
    return variationDetail(flagKey, context, fallback).value();
  }

  /**
   * As {@link #variation}, with the variation index and the reason.
   *
   * @param flagKey the flag
   * @param context the context
   * @param fallback the fallback
   * @return the detail
   */
  public Detail<Object> variationDetail(String flagKey, Object context, Object fallback) {
    return typed(flagKey, context, fallback, wantOf(fallback));
  }

  /**
   * Stops the SDK: ends the ruleset and stream requests, stops the flush loop, waits for both,
   * sends the final usage flush (which does not wait out a {@code Retry-After}) and returns, within
   * about twice {@code requestTimeout}. Safe to call twice or from many threads at once. Called
   * from one of the SDK's own threads, or again from the thread already closing, it does not wait.
   * Evaluation still answers afterwards.
   */
  @Override
  public void close() {
    boolean first = closeStarted.compareAndSet(false, true);
    if (first) {
      if (onOwnThread()) {
        Thread helper = new Thread(this::shutdown, "toggleslash-close");
        helper.setDaemon(true);
        closer = helper;
        helper.start();
        return;
      }
      closer = Thread.currentThread();
      shutdown();
      return;
    }
    if (onOwnThread() || Thread.currentThread() == closer) {
      return;
    }
    try {
      doneLatch.await();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  // ------------------------------------------------------------------ evaluation

  private static Want wantOf(Object fallback) {
    if (fallback == null) {
      return Want.ANY;
    } else if (fallback instanceof Boolean) {
      return Want.BOOL;
    } else if (fallback instanceof Number) {
      return Want.NUMBER;
    } else if (fallback instanceof CharSequence) {
      return Want.STRING;
    } else if (fallback instanceof Collection || fallback instanceof Object[]) {
      return Want.ARRAY;
    } else if (fallback instanceof Map) {
      return Want.OBJECT;
    }
    return Want.OTHER;
  }

  private static boolean matches(Want want, Object served) {
    if (want == Want.ANY) {
      return true;
    }
    if (served == null) {
      return false;
    }
    switch (want) {
      case BOOL:
        return served instanceof Boolean;
      case STRING:
        return served instanceof String;
      case NUMBER:
        return served instanceof Double;
      case INT:
        return served instanceof Double d && isInt64(d);
      case ARRAY:
        return served instanceof List;
      case OBJECT:
        return served instanceof Map;
      default:
        return false;
    }
  }

  private static boolean isInt64(double d) {
    return d == Math.rint(d) && d >= -0x1p63 && d < 0x1p63;
  }

  @SuppressWarnings("unchecked")
  private <T> Detail<T> typed(String flagKey, Object context, Object fallback, Want want) {
    Held snapshot = held;
    Object result = fallback;
    Integer index = null;
    Reason reason;
    boolean counted = false;
    try {
      if (snapshot == null) {
        reason = Reason.error(ErrorCode.NOT_READY);
      } else {
        EvalResult eval = evaluate(snapshot.ruleset(), flagKey, context);
        if (eval.variationIndex() == null) {
          reason = eval.reason();
        } else if (!matches(want, eval.value())) {
          reason = Reason.error(ErrorCode.TYPE_MISMATCH);
        } else {
          result =
              want == Want.INT
                  ? (Object) Long.valueOf(((Double) eval.value()).longValue())
                  : eval.value();
          index = eval.variationIndex();
          reason = eval.reason();
          counted = true;
        }
      }
    } catch (Exception | StackOverflowError e) {
      result = fallback;
      index = null;
      counted = false;
      reason = Reason.error(ErrorCode.EVALUATION_FAILED);
      reportError(e);
    }
    if (counted && !closedFlag) {
      usage.record(flagKey, index, options.clock().instant());
    }
    runHook(flagKey, context, result, index, reason, snapshot);
    return new Detail<>((T) result, index, reason);
  }

  private EvalResult evaluate(Ruleset ruleset, String flagKey, Object context) {
    Object model;
    try {
      model = toModel(context, 0);
    } catch (InvalidContext e) {
      return EvalResult.failure(ErrorCode.INVALID_CONTEXT);
    }
    return options.evaluator().run(ruleset, flagKey, model);
  }

  private static final class InvalidContext extends Exception {
    private static final long serialVersionUID = 1L;

    InvalidContext() {
      super(null, null, false, false);
    }
  }

  /** Copies a caller's context into the JSON model the evaluator reads. */
  private static Object toModel(Object value, int depth) throws InvalidContext {
    if (depth > MAX_CONTEXT_DEPTH) {
      throw new InvalidContext();
    }
    if (value == null || value instanceof Boolean || value instanceof String) {
      return value;
    } else if (value instanceof Number n) {
      return n.doubleValue();
    } else if (value instanceof CharSequence s) {
      return s.toString();
    } else if (value instanceof Map<?, ?> m) {
      Map<String, Object> out = new LinkedHashMap<>();
      for (Map.Entry<?, ?> entry : m.entrySet()) {
        if (!(entry.getKey() instanceof String key)) {
          throw new InvalidContext();
        }
        out.put(key, toModel(entry.getValue(), depth + 1));
      }
      return out;
    } else if (value instanceof Collection<?> c) {
      List<Object> out = new ArrayList<>(c.size());
      for (Object item : c) {
        out.add(toModel(item, depth + 1));
      }
      return out;
    } else if (value instanceof Object[] array) {
      List<Object> out = new ArrayList<>(array.length);
      for (Object item : array) {
        out.add(toModel(item, depth + 1));
      }
      return out;
    }
    throw new InvalidContext();
  }

  private void runHook(
      String flagKey, Object context, Object value, Integer index, Reason reason, Held snapshot) {
    if (options.afterEvaluation() == null) {
      return;
    }
    try {
      Long version = null;
      String etag = null;
      if (snapshot != null) {
        etag = snapshot.etag();
        Double v = snapshot.ruleset().versionOf(flagKey);
        if (v != null && isInt64(v)) {
          version = v.longValue();
        }
      }
      options
          .afterEvaluation()
          .accept(new EvaluationEvent(flagKey, context, value, index, reason, etag, version));
    } catch (Exception | StackOverflowError e) {
      reportError(e);
    }
  }

  private void reportError(Throwable error) {
    if (options.onError() == null) {
      return;
    }
    try {
      options.onError().accept(error);
    } catch (Exception | StackOverflowError ignored) {
      // An error in onError is ignored (contract section 10).
    }
  }

  private void fail(String code, Throwable cause) {
    mutate(s -> new Status(s.etag(), s.lastSyncAt(), s.stream(), code));
    reportError(new ToggleSlashException(code, "ToggleSlash request failed: " + code, cause));
  }

  // ------------------------------------------------------------------ state

  private void mutate(UnaryOperator<Status> change) {
    synchronized (stateLock) {
      Status next = change.apply(status);
      if (closedFlag) {
        next = new Status(next.etag(), next.lastSyncAt(), StreamState.CLOSED, next.lastError());
      }
      status = next;
    }
  }

  private void setStream(StreamState state) {
    mutate(s -> new Status(s.etag(), s.lastSyncAt(), state, s.lastError()));
  }

  private void synced() {
    Instant now = options.clock().instant();
    mutate(s -> new Status(s.etag(), now, s.stream(), null));
  }

  private void adopt(Ruleset ruleset, String etag) {
    Instant now = options.clock().instant();
    synchronized (stateLock) {
      held = new Held(ruleset, etag);
      Status s = status;
      Status next = new Status(etag, now, s.stream(), null);
      if (closedFlag) {
        next = new Status(next.etag(), next.lastSyncAt(), StreamState.CLOSED, null);
      }
      status = next;
    }
  }

  private boolean onOwnThread() {
    return Thread.currentThread() instanceof SdkThread t && t.owner == this;
  }

  // ------------------------------------------------------------------ the sync loop

  private void syncMain() {
    try {
      try {
        fetchRuleset();
      } finally {
        ready.complete(held != null);
      }
      loop();
    } catch (Exception | StackOverflowError e) {
      reportError(e);
    }
  }

  private void loop() {
    int failures = 0;
    while (!closedFlag) {
      if (options.stream() && !awaitHold(rulesetHoldUntil)) {
        return;
      }
      boolean connected = options.stream() && followStream();
      if (closedFlag) {
        return;
      }
      failures = connected ? 0 : failures + 1;
      long wait = Math.max(delayMillis(failures), holdRemaining(rulesetHoldUntil));
      if (!sleep(wait)) {
        return;
      }
      if (!connected) {
        fetchRuleset();
      }
    }
  }

  private long delayMillis(int failures) {
    long poll = options.pollInterval().toMillis();
    if (!options.stream()) {
      return poll;
    }
    long ceiling = Math.min(poll, 1000L << Math.min(failures, 16));
    return Math.round(ceiling * options.jitter().getAsDouble());
  }

  private long holdRemaining(long until) {
    return Math.max(0L, until - options.clock().millis());
  }

  /** Waits until a hold has passed; false if the client closed meanwhile. */
  private boolean awaitHold(long until) {
    long remaining = holdRemaining(until);
    while (remaining > 0) {
      if (!sleep(remaining)) {
        return false;
      }
      remaining = holdRemaining(until);
    }
    return !closedFlag;
  }

  /** Sleeps; false if the client closed (or the thread was interrupted) before the time was up. */
  private boolean sleep(long millis) {
    try {
      return !closeLatch.await(Math.max(0L, millis), TimeUnit.MILLISECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return false;
    }
  }

  /** Reads {@code Retry-After} of a 429 and extends a hold; the result is the new hold time. */
  private long extendedHold(long current, String header) {
    if (header == null) {
      return current;
    }
    String text = header.strip();
    if (text.isEmpty()) {
      return current;
    }
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      if (c < '0' || c > '9') {
        return current;
      }
    }
    int first = 0;
    while (first < text.length() - 1 && text.charAt(first) == '0') {
      first++;
    }
    String digits = text.substring(first);
    long seconds =
        digits.length() > 6
            ? RETRY_AFTER_CAP_SECONDS
            : Math.min(Long.parseLong(digits), RETRY_AFTER_CAP_SECONDS);
    return Math.max(current, options.clock().millis() + seconds * 1000L);
  }

  /** One ruleset request; true if it was a sync. */
  private boolean fetchRuleset() {
    Http.Call call = http.call(true);
    try {
      HttpRequest.Builder builder = http.request("/sdk/v1/ruleset").GET();
      Held current = held;
      if (current != null && current.etag() != null && !current.etag().isEmpty()) {
        builder.header("If-None-Match", current.etag());
      }
      Http.Result result =
          http.fetch(call, builder.build(), s -> s == 200, Http.deadline(options.requestTimeout()));
      switch (result.status()) {
        case 200:
          return adoptBody(result);
        case 304:
          if (held != null) {
            synced();
            return true;
          }
          fail("http_304", null);
          return false;
        case 429:
          rulesetHoldUntil = extendedHold(rulesetHoldUntil, result.retryAfter());
          fail("http_429", null);
          return false;
        default:
          fail("http_" + result.status(), null);
          return false;
      }
    } catch (Http.Failure f) {
      if (!f.aborted && !closedFlag) {
        fail(f.code, f.getCause());
      }
      return false;
    } finally {
      call.finish();
    }
  }

  private boolean adoptBody(Http.Result result) {
    Object root;
    try {
      String text = Utf8.decode(result.body());
      if (text.startsWith("﻿")) {
        text = text.substring(1);
      }
      root = Json.parse(text);
    } catch (IllegalArgumentException e) {
      fail("bad_ruleset", e);
      return false;
    }
    if (!(root instanceof Map<?, ?> map)
        || !(map.get("flags") instanceof List)
        || !(map.get("segments") instanceof List)) {
      fail("bad_ruleset", null);
      return false;
    }
    String etag = result.etag() == null || result.etag().isEmpty() ? null : result.etag();
    adopt(Ruleset.load(root), etag);
    return true;
  }

  /** Follows one stream until it ends; true if it connected and was not ended by a failed fetch. */
  private boolean followStream() {
    setStream(StreamState.CONNECTING);
    Http.Call call = http.call(true);
    boolean connected = false;
    try {
      LinkedBlockingQueue<Object> queue = new LinkedBlockingQueue<>();
      AtomicLong lastByte = new AtomicLong(System.nanoTime());
      Sse parser = new Sse(queue::add);
      Http.Exchange exchange;
      try {
        HttpRequest request =
            http.request("/sdk/v1/stream").header("Accept", "text/event-stream").GET().build();
        exchange =
            http.start(
                call,
                request,
                s -> s == 200,
                bytes -> {
                  lastByte.set(System.nanoTime());
                  parser.feed(bytes, bytes.length);
                },
                Http.deadline(options.requestTimeout()));
      } catch (Http.Failure f) {
        if (!f.aborted && !closedFlag) {
          fail(f.code, f.getCause());
        }
        return false;
      }
      if (exchange.status() != 200) {
        if (exchange.status() == 429) {
          rulesetHoldUntil = extendedHold(rulesetHoldUntil, exchange.retryAfter());
        }
        fail("http_" + exchange.status(), null);
        return false;
      }
      lastByte.set(System.nanoTime());
      exchange.done().whenComplete((v, error) -> queue.add(new End(error)));
      connected = readEvents(call, queue, lastByte);
      return connected;
    } finally {
      call.abort();
      call.finish();
      if (!closedFlag) {
        setStream(StreamState.DISCONNECTED);
      }
    }
  }

  private boolean readEvents(
      Http.Call call, LinkedBlockingQueue<Object> queue, AtomicLong lastByte) {
    boolean connected = false;
    long watchdog = options.watchdog().toNanos();
    while (!closedFlag) {
      long idle = watchdog - (System.nanoTime() - lastByte.get());
      if (idle <= 0) {
        call.abort();
        fail("timeout", null);
        return connected;
      }
      Object item;
      try {
        item = queue.poll(idle, TimeUnit.NANOSECONDS);
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
        return connected;
      }
      if (item == null) {
        continue;
      }
      if (item instanceof End end) {
        if (end.error() != null && !call.aborted && !closedFlag) {
          fail("network", end.error());
        }
        return connected;
      }
      Sse.Event event = (Sse.Event) item;
      boolean isConnected = event.name().equals("connected");
      if (!isConnected && !event.name().equals("changed")) {
        continue;
      }
      if (isConnected) {
        mutate(s -> new Status(s.etag(), s.lastSyncAt(), StreamState.CONNECTED, null));
        connected = true;
      }
      Held current = held;
      if (current != null
          && current.etag() != null
          && !current.etag().isEmpty()
          && current.etag().equals(event.data())) {
        synced();
      } else if (!fetchRuleset()) {
        return false;
      }
    }
    return connected;
  }

  // ------------------------------------------------------------------ usage

  private void flushMain() {
    try {
      while (!closedFlag) {
        if (!sleep(options.flushInterval().toMillis())) {
          return;
        }
        flush(false);
      }
    } catch (Exception | StackOverflowError e) {
      reportError(e);
    }
  }

  private enum Outcome {
    SENT,
    KEPT,
    DROPPED
  }

  /**
   * Sends every pending count. {@code closing} makes a hold lose the counts instead of keeping
   * them.
   */
  private void flush(boolean closing) {
    flushLock.lock();
    try {
      if (holdRemaining(usageHoldUntil) > 0) {
        if (closing) {
          int lost = usage.discard();
          if (lost > 0) {
            reportError(
                new ToggleSlashException(
                    "usage_lost",
                    "closed with " + lost + " usage counts unsent: the server asked to wait",
                    null));
          }
        }
        return;
      }
      List<Usage.Entry> entries = usage.take();
      for (int from = 0; from < entries.size(); from += MAX_ENTRIES_PER_REQUEST) {
        int to = Math.min(from + MAX_ENTRIES_PER_REQUEST, entries.size());
        Outcome outcome = send(entries.subList(from, to));
        if (outcome == Outcome.KEPT) {
          usage.restore(entries.subList(from, entries.size()));
          return;
        }
      }
    } finally {
      flushLock.unlock();
    }
  }

  private Outcome send(List<Usage.Entry> chunk) {
    Http.Call call = http.call(false);
    try {
      byte[] body = Utf8.encode(Usage.body(chunk));
      HttpRequest request =
          http.request("/sdk/v1/events")
              .header("Content-Type", "application/json")
              .POST(HttpRequest.BodyPublishers.ofByteArray(body))
              .build();
      Http.Result result =
          http.fetch(call, request, s -> false, Http.deadline(options.requestTimeout()));
      int code = result.status();
      if (code >= 200 && code < 300) {
        return Outcome.SENT;
      }
      if (code == 429) {
        usageHoldUntil = extendedHold(usageHoldUntil, result.retryAfter());
      }
      reportError(
          new ToggleSlashException("http_" + code, "usage request failed: http_" + code, null));
      boolean keep = code >= 500 || code == 408 || code == 429;
      return keep ? Outcome.KEPT : Outcome.DROPPED;
    } catch (Http.Failure f) {
      reportError(
          new ToggleSlashException(f.code, "usage request failed: " + f.code, f.getCause()));
      return Outcome.KEPT;
    } finally {
      call.finish();
    }
  }

  // ------------------------------------------------------------------ close

  private void shutdown() {
    try {
      synchronized (stateLock) {
        closedFlag = true;
        Status s = status;
        status = new Status(s.etag(), s.lastSyncAt(), StreamState.CLOSED, s.lastError());
      }
      usage.close();
      closeLatch.countDown();
      http.abortSync();
      long limit = options.requestTimeout().toMillis();
      join(syncThread, limit);
      join(flushThread, limit);
      if (flushThread != null && flushThread.isAlive()) {
        http.abortAll();
      }
      flush(true);
    } catch (Exception | StackOverflowError e) {
      reportError(e);
    } finally {
      ready.complete(false);
      doneLatch.countDown();
    }
  }

  private void join(Thread thread, long millis) {
    if (thread == null || thread == Thread.currentThread()) {
      return;
    }
    try {
      thread.join(millis);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  // ------------------------------------------------------------------ seams for the tests

  long rulesetHoldUntil() {
    return rulesetHoldUntil;
  }

  long usageHoldUntil() {
    return usageHoldUntil;
  }

  /** Sends the pending counts now, as the flush loop would. */
  void flushNow() {
    flush(false);
  }
}
