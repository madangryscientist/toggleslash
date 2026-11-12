package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.JsonException;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;

/**
 * Keeps one ruleset current: it fetches with {@code If-None-Match}, follows the stream when it can
 * and polls when it cannot. Nothing in here throws; every failure goes to {@code onError} and
 * leaves the last good ruleset in place. It runs on one thread, so the ruleset, the ETag and the
 * state change only there, and readers see them through one atomic reference.
 */
final class Sync {
  private static final long BACKOFF_BASE_MS = 1_000;

  private final String base;
  private final String sdkKey;
  private final Http http;
  private final Clock clock;
  private final DoubleSupplier jitter;
  private final Reporter reporter;
  private final boolean stream;
  private final long pollMs;
  private final long timeoutMs;
  private final long idleMs;
  private final long capMs;
  private final Stop stop;
  private final Hold hold = new Hold();
  final AtomicReference<Snapshot> state;

  Sync(
      String base,
      String sdkKey,
      Http http,
      Clock clock,
      DoubleSupplier jitter,
      Reporter reporter,
      Stop stop,
      boolean stream,
      long pollMs,
      long timeoutMs,
      long idleMs,
      long capMs) {
    this.base = base;
    this.sdkKey = sdkKey;
    this.http = http;
    this.clock = clock;
    this.jitter = jitter;
    this.reporter = reporter;
    this.stop = stop;
    this.stream = stream;
    this.pollMs = pollMs;
    this.timeoutMs = timeoutMs;
    this.idleMs = idleMs;
    this.capMs = capMs;
    this.state =
        new AtomicReference<>(
            new Snapshot(
                null, null, null, stream ? StreamState.CONNECTING : StreamState.OFF, null));
  }

  /**
   * Loads once, then follows until {@code stop}. {@code firstLoad} is told whether a ruleset is
   * held as soon as the first attempt has an outcome.
   */
  void run(Consumer<Boolean> firstLoad) {
    refresh(firstLoad);
    long failures = 0;
    while (!stop.isSignalled()) {
      // A 429 on the ruleset says wait, and without this the stream would be asked straight after.
      if (stream) {
        sleep(0);
      }
      if (stop.isSignalled()) {
        return;
      }
      boolean connected = stream && follow();
      failures = connected ? 0 : failures + 1;
      sleep(delay(failures));
      // Sleep first: the ruleset was only just loaded or the stream only just failed. A stream that
      // was up needs no poll, since reconnecting announces the tag and catches up on its own.
      if (!connected && !stop.isSignalled()) {
        refresh(null);
      }
    }
  }

  /**
   * Sleeps for {@code ms}, or until the server's {@code Retry-After} has passed if that is later.
   */
  private void sleep(long ms) {
    long wait = Math.max(ms, hold.remaining(clock.now()));
    if (wait <= 0 || stop.isSignalled()) {
      return;
    }
    try {
      clock.sleep(wait, stop);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  /** Exponential from a second up to the poll interval, jittered; with no stream, the interval. */
  private long delay(long failures) {
    if (!stream) {
      return pollMs;
    }
    double ceiling =
        Math.min((double) pollMs, BACKOFF_BASE_MS * Math.pow(2, Math.min(failures, 16)));
    return (long) (ceiling * (0.5 + jitter.getAsDouble() / 2));
  }

  /** Remembers what a 429 asked for; a shorter answer never shortens a longer wait. */
  private void honourRetryAfter(int status, String header) {
    if (status != 429) {
      return;
    }
    long wait = Hold.parse(header, capMs);
    if (wait >= 0) {
      hold.extend(clock.now() + wait);
    }
  }

  private void synced() {
    long now = clock.now();
    state.updateAndGet(s -> s.synced(now));
  }

  private void fail(ToggleSlashException failure) {
    if (record(failure)) {
      reporter.report(failure);
    }
  }

  /** Puts the error code into the snapshot; false when the client is closing and it is dropped. */
  private boolean record(ToggleSlashException failure) {
    if (stop.isSignalled()) {
      return false;
    }
    state.updateAndGet(s -> s.withError(failure.code()));
    return true;
  }

  private static ToggleSlashException httpError(String method, String route, int status) {
    return new ToggleSlashException(
        "http_" + status, method + " " + route + " answered " + status, null);
  }

  private static boolean hasRulesetShape(Object body) {
    if (!(body instanceof Map)) {
      return false;
    }
    Map<?, ?> map = (Map<?, ?>) body;
    return map.get("flags") instanceof List && map.get("segments") instanceof List;
  }

  /** One conditional fetch of the ruleset. True when it ended with the ruleset current. */
  boolean refresh(Consumer<Boolean> firstLoad) {
    ToggleSlashException failure = null;
    boolean ok = false;
    try {
      attempt();
      ok = true;
    } catch (Http.Aborted e) {
      if (firstLoad != null) {
        firstLoad.accept(state.get().ruleset() != null);
      }
      return false;
    } catch (ToggleSlashException e) {
      failure = e;
    } catch (RuntimeException e) {
      failure = new ToggleSlashException("network", "GET /sdk/v1/ruleset failed", e);
    }
    // The error is in the snapshot before ready settles, so a caller that sees ready() answer false
    // sees the cause on status. onError is told after, and the contract sets no order between them.
    boolean recorded = failure != null && record(failure);
    if (firstLoad != null) {
      firstLoad.accept(state.get().ruleset() != null);
    }
    if (recorded) {
      reporter.report(failure);
    }
    return ok;
  }

  private void attempt() {
    Snapshot held = state.get();
    Map<String, String> headers = new LinkedHashMap<>();
    headers.put("Authorization", "Bearer " + sdkKey);
    if (held.etag() != null && held.ruleset() != null) {
      headers.put("If-None-Match", held.etag());
    }
    Http.Answer answer =
        http.request("GET", base + "/sdk/v1/ruleset", headers, null, timeoutMs, true);
    if (answer.status() == 304) {
      // A 304 answers If-None-Match, which is sent only with a ruleset held; with none it is odd.
      if (held.ruleset() == null) {
        throw new ToggleSlashException(
            "http_304", "GET /sdk/v1/ruleset answered 304 with no ruleset held", null);
      }
      synced();
      return;
    }
    if (answer.status() != 200) {
      honourRetryAfter(answer.status(), answer.header("Retry-After"));
      throw httpError("GET", "/sdk/v1/ruleset", answer.status());
    }
    Object body;
    try {
      body = Json.parse(answer.body(), 0, answer.body().length);
    } catch (JsonException e) {
      throw new ToggleSlashException(Status.BAD_RULESET, "GET /sdk/v1/ruleset sent no JSON", e);
    }
    if (!hasRulesetShape(body)) {
      throw new ToggleSlashException(
          Status.BAD_RULESET, "GET /sdk/v1/ruleset sent no flags and segments", null);
    }
    Ruleset ruleset = Ruleset.create(body);
    String etag = answer.header("ETag");
    String tag = etag == null || etag.isEmpty() ? null : etag;
    long now = clock.now();
    state.updateAndGet(s -> s.loaded(ruleset, tag, now));
  }

  /**
   * Follows {@code GET /sdk/v1/stream} until it ends or fails. True if it connected, so the caller
   * can tell a stream that was up from one that never was.
   */
  private boolean follow() {
    state.updateAndGet(s -> s.withStream(StreamState.CONNECTING));
    boolean[] connected = {false};
    Http.Opened opened = null;
    try {
      Map<String, String> headers = new LinkedHashMap<>();
      headers.put("Authorization", "Bearer " + sdkKey);
      headers.put("Accept", "text/event-stream");
      opened = http.open(base + "/sdk/v1/stream", headers, timeoutMs);
      if (opened.status != 200) {
        honourRetryAfter(opened.status, opened.header("Retry-After"));
        throw httpError("GET", "/sdk/v1/stream", opened.status);
      }
      pump(opened.body, connected);
    } catch (Http.Aborted e) {
      // close cancelled it: nothing to report.
    } catch (ToggleSlashException e) {
      fail(e);
    } catch (RuntimeException e) {
      fail(new ToggleSlashException("network", "GET /sdk/v1/stream failed", e));
    } finally {
      if (opened != null) {
        http.release(opened);
      }
      state.updateAndGet(s -> s.withStream(StreamState.DISCONNECTED));
    }
    return connected[0];
  }

  /** Reads events until the stream ends; going quiet for the idle limit drops it. */
  private void pump(Http.StreamBody body, boolean[] connected) {
    Utf8.Decoder decoder = new Utf8.Decoder();
    SseParser parser = new SseParser();
    StringBuilder text = new StringBuilder();
    List<SseParser.Event> events = new ArrayList<>();
    for (; ; ) {
      byte[] chunk = body.next(idleMs);
      if (chunk == null) {
        return;
      }
      text.setLength(0);
      decoder.write(chunk, 0, chunk.length, text);
      parser.feed(text, events);
      for (SseParser.Event event : events) {
        boolean isConnected = event.name().equals("connected");
        if (!isConnected && !event.name().equals("changed")) {
          continue;
        }
        if (isConnected) {
          connected[0] = true;
          state.updateAndGet(Snapshot::withConnected);
        }
        if (!onTag(event.data())) {
          // The fetch the stream asked for failed, and a stream that stays up would never retry it.
          // End it and count it as not connected, so the backoff and the poll take over.
          connected[0] = false;
          return;
        }
      }
      events.clear();
    }
  }

  /** The stream names the ruleset's ETag; fetch only when it is not the one held. */
  private boolean onTag(String tag) {
    Snapshot held = state.get();
    if (held.ruleset() != null && held.etag() != null && tag.equals(held.etag())) {
      synced();
      return true;
    }
    return refresh(null);
  }
}
