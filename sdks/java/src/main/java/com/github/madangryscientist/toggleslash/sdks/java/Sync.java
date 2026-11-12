package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.JsonException;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.net.URI;
import java.net.http.HttpRequest;
import java.time.Instant;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.UnaryOperator;

/** Keeps the ruleset current: the first load, then the stream and the polls. */
final class Sync implements Runnable {
  /** Everything that is read together: one immutable snapshot. */
  record State(
      Ruleset ruleset, String etag, Instant lastSyncAt, String lastError, StreamState stream) {}

  private final Options options;
  private final Http http;
  private final Closing closing;
  private final Reporter reporter;
  private final AtomicReference<State> state;
  private final CompletableFuture<Boolean> ready = new CompletableFuture<>();
  private volatile long holdUntil = Long.MIN_VALUE;

  Sync(Options options, Http http, Closing closing, Reporter reporter) {
    this.options = options;
    this.http = http;
    this.closing = closing;
    this.reporter = reporter;
    this.state =
        new AtomicReference<>(
            new State(
                null, null, null, null, options.stream ? StreamState.CONNECTING : StreamState.OFF));
  }

  State state() {
    return state.get();
  }

  CompletableFuture<Boolean> ready() {
    return ready;
  }

  void markClosed() {
    state.updateAndGet(
        s -> new State(s.ruleset(), s.etag(), s.lastSyncAt(), s.lastError(), StreamState.CLOSED));
    ready.complete(false);
  }

  private void update(UnaryOperator<State> change) {
    state.updateAndGet(
        s -> s.stream() == StreamState.CLOSED || closing.isClosed() ? s : change.apply(s));
  }

  private void setStream(StreamState stream) {
    update(s -> new State(s.ruleset(), s.etag(), s.lastSyncAt(), s.lastError(), stream));
  }

  private void synced() {
    Instant now = Instant.ofEpochMilli(options.clock.wallMillis());
    update(s -> new State(s.ruleset(), s.etag(), now, null, s.stream()));
  }

  private void failed(String code, String what) {
    if (closing.isClosed()) {
      return;
    }
    update(s -> new State(s.ruleset(), s.etag(), s.lastSyncAt(), code, s.stream()));
    reporter.report(what + " failed: " + code);
  }

  /**
   * A stream failure: the error and the end of the stream are one state change, so a reader never
   * sees the error beside a stream still shown as connected.
   */
  private void streamFailed(String code) {
    if (closing.isClosed()) {
      return;
    }
    update(s -> new State(s.ruleset(), s.etag(), s.lastSyncAt(), code, StreamState.DISCONNECTED));
    reporter.report("stream failed: " + code);
  }

  private HttpRequest.Builder request(String path, String accept) {
    return HttpRequest.newBuilder(URI.create(options.url + path))
        .header("Authorization", "Bearer " + options.sdkKey)
        .header("Accept", accept)
        .GET();
  }

  private void hold(String retryAfter) {
    long seconds = RetryAfter.seconds(retryAfter);
    if (seconds >= 0) {
      holdUntil = Math.max(holdUntil, options.clock.monoMillis() + seconds * 1000L);
    }
  }

  private long holdRemaining() {
    return holdUntil == Long.MIN_VALUE ? 0 : Math.max(0, holdUntil - options.clock.monoMillis());
  }

  private static boolean isRuleset(Object parsed) {
    return parsed instanceof Map
        && ((Map<?, ?>) parsed).get("flags") instanceof List
        && ((Map<?, ?>) parsed).get("segments") instanceof List;
  }

  /** The ruleset request. True when it was a sync. */
  boolean fetchRuleset() {
    if (closing.isClosed()) {
      return false;
    }
    Http.Response response;
    try {
      State held = state.get();
      HttpRequest.Builder builder = request("/sdk/v1/ruleset", "application/json");
      if (held.ruleset() != null && held.etag() != null && !held.etag().isEmpty()) {
        builder.header("If-None-Match", held.etag());
      }
      response = http.send(builder, options.requestTimeout);
    } catch (Http.Failure failure) {
      failed(failure.code, "ruleset request");
      return false;
    } catch (RuntimeException e) {
      failed(Status.NETWORK, "ruleset request");
      return false;
    }
    if (closing.isClosed()) {
      return false;
    }
    int status = response.status;
    if (status == 200) {
      return load(response);
    }
    if (status == 304 && state.get().ruleset() != null) {
      synced();
      return true;
    }
    if (status == 429) {
      hold(response.retryAfter);
    }
    failed("http_" + status, "ruleset request");
    return false;
  }

  private boolean load(Http.Response response) {
    Ruleset ruleset;
    try {
      String text = Utf8.decode(response.body);
      if (!text.isEmpty() && text.charAt(0) == '﻿') {
        text = text.substring(1);
      }
      Object parsed = Json.parse(text);
      if (!isRuleset(parsed)) {
        failed(Status.BAD_RULESET, "ruleset request");
        return false;
      }
      ruleset = Ruleset.create(parsed);
    } catch (JsonException e) {
      failed(Status.BAD_RULESET, "ruleset request");
      return false;
    } catch (RuntimeException e) {
      failed(Status.BAD_RULESET, "ruleset request");
      return false;
    }
    String etag = response.etag == null || response.etag.isEmpty() ? null : response.etag;
    Instant now = Instant.ofEpochMilli(options.clock.wallMillis());
    update(s -> new State(ruleset, etag, now, null, s.stream()));
    return true;
  }

  @Override
  public void run() {
    Client.markSdkThread();
    try {
      fetchRuleset();
      ready.complete(state.get().ruleset() != null);
      loop();
    } catch (Throwable error) {
      ready.complete(state.get().ruleset() != null);
      if (!closing.isClosed()) {
        reporter.report(error);
      }
    }
  }

  private long delayMillis(long failures) {
    long pollMillis = options.pollInterval.toMillis();
    if (!options.stream) {
      return pollMillis;
    }
    long ceiling = Math.min(pollMillis, 1000L << Math.min(failures, 16));
    return (long) (ceiling * options.jitter.getAsDouble());
  }

  private void sleep(long millis) throws InterruptedException {
    if (millis > 0) {
      options.clock.sleep(millis, closing);
    }
  }

  private void loop() throws InterruptedException {
    long failures = 0;
    while (!closing.isClosed()) {
      if (options.stream) {
        sleep(holdRemaining());
        if (closing.isClosed()) {
          return;
        }
      }
      boolean connected = false;
      if (options.stream) {
        try {
          connected = follow();
        } catch (RuntimeException e) {
          streamFailed(Status.NETWORK);
        }
        setStream(StreamState.DISCONNECTED);
      }
      if (closing.isClosed()) {
        return;
      }
      failures = connected ? 0 : failures + 1;
      sleep(Math.max(delayMillis(failures), holdRemaining()));
      if (closing.isClosed()) {
        return;
      }
      if (!connected) {
        try {
          fetchRuleset();
        } catch (RuntimeException e) {
          failed(Status.NETWORK, "ruleset request");
        }
      }
    }
  }

  /** Follows the stream until it ends. True when its connected event came and no fetch failed. */
  private boolean follow() throws InterruptedException {
    setStream(StreamState.CONNECTING);
    Http.StreamResponse stream;
    try {
      stream = http.open(request("/sdk/v1/stream", "text/event-stream"), options.requestTimeout);
    } catch (Http.Failure failure) {
      streamFailed(failure.code);
      return false;
    }
    try {
      return read(stream);
    } finally {
      stream.cancel();
    }
  }

  private boolean read(Http.StreamResponse stream) throws InterruptedException {
    if (closing.isClosed()) {
      return false;
    }
    if (stream.status != 200) {
      if (stream.status == 429) {
        hold(stream.retryAfter);
      }
      streamFailed("http_" + stream.status);
      return false;
    }
    boolean[] connected = new boolean[1];
    Utf8.Decoder decoder = new Utf8.Decoder();
    SseParser parser = new SseParser((name, data) -> onEvent(name, data, connected));
    long watchdog = options.watchdog.toMillis();
    StringBuilder text = new StringBuilder();
    while (true) {
      Object item = stream.queue.poll(watchdog, TimeUnit.MILLISECONDS);
      if (closing.isClosed() || item == Http.ABORTED) {
        return false;
      }
      if (item == null) {
        streamFailed(Status.TIMEOUT);
        return connected[0];
      }
      if (item == Http.END) {
        return connected[0];
      }
      if (item instanceof Throwable) {
        streamFailed(Status.NETWORK);
        return connected[0];
      }
      text.setLength(0);
      decoder.decode((byte[]) item, 0, ((byte[]) item).length, text);
      boolean go = parser.accept(text);
      if (!go) {
        return connected[0];
      }
      stream.more();
    }
  }

  private boolean onEvent(String name, String tag, boolean[] connected) {
    boolean isConnected = name.equals("connected");
    if (!isConnected && !name.equals("changed")) {
      return true;
    }
    if (isConnected) {
      connected[0] = true;
      update(s -> new State(s.ruleset(), s.etag(), s.lastSyncAt(), null, StreamState.CONNECTED));
    }
    State held = state.get();
    if (held.ruleset() != null
        && held.etag() != null
        && !held.etag().isEmpty()
        && held.etag().equals(tag)) {
      synced();
      return true;
    }
    if (!fetchRuleset()) {
      connected[0] = false;
      return false;
    }
    return true;
  }
}
