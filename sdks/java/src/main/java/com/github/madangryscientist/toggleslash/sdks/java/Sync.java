package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.OptionalLong;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;

/**
 * Keeps one ruleset current: it fetches with {@code If-None-Match}, follows the stream when it can
 * and polls when it cannot. Nothing in here throws at a caller; every failure goes to the error
 * callback and leaves the last good ruleset in place. The state (ruleset and tag together, the last
 * sync, the last error and the stream's state) changes under {@link #state}; no lock is held across
 * I/O or a callback.
 */
final class Sync {
  /** The ruleset held and the ETag it came with (null when the server sent none). */
  record Snapshot(Ruleset ruleset, String etag) {}

  private static final Duration BACKOFF_BASE = Duration.ofSeconds(1);

  private final Http http;
  private final String baseUrl;
  private final String sdkKey;
  private final Duration requestTimeout;
  private final Duration pollInterval;
  private final Duration streamIdle;
  private final boolean streamOn;
  private final Clock clock;
  private final DoubleSupplier jitter;
  private final Hold hold;
  private final Consumer<Throwable> report;
  private final Runnable onFirstLoad;
  private final CountDownLatch stop = new CountDownLatch(1);
  private final Object state = new Object();

  private volatile Snapshot snapshot;
  private volatile boolean stopped;
  private Instant lastSyncAt;
  private String lastError;
  private StreamState streamState;

  Sync(
      Http http,
      Options options,
      String baseUrl,
      Hold hold,
      Consumer<Throwable> report,
      Runnable onFirstLoad) {
    this.http = http;
    this.baseUrl = baseUrl;
    this.sdkKey = options.sdkKey();
    this.requestTimeout = options.requestTimeout();
    this.pollInterval = options.pollInterval();
    this.streamIdle = options.streamIdle();
    this.streamOn = options.stream();
    this.clock = options.clock();
    this.jitter = options.jitter();
    this.hold = hold;
    this.report = report;
    this.onFirstLoad = onFirstLoad;
    this.streamState = streamOn ? StreamState.CONNECTING : StreamState.OFF;
  }

  /** The ruleset held with its tag; null before the first load. One read, one consistent pair. */
  Snapshot snapshot() {
    return snapshot;
  }

  Status status() {
    synchronized (state) {
      Snapshot held = snapshot;
      return new Status(
          Optional.ofNullable(held == null ? null : held.etag()),
          Optional.ofNullable(lastSyncAt),
          stopped ? StreamState.CLOSED : streamState,
          Optional.ofNullable(lastError));
    }
  }

  /** Stops the loop and abandons the request in flight. */
  void stop() {
    synchronized (state) {
      stopped = true;
    }
    stop.countDown();
    http.abort();
  }

  /** The body of the sync thread: load once, then follow until stopped. */
  void run() {
    try {
      refresh();
    } finally {
      onFirstLoad.run();
    }
    int failures = 0;
    while (!stopped) {
      if (streamOn && !sleep(0)) {
        return;
      }
      boolean connected = streamOn && follow();
      failures = connected ? 0 : failures + 1;
      if (!sleep(delay(failures).toNanos())) {
        return;
      }
      // Sleep first: the ruleset was only just loaded, or the stream only just failed. A stream
      // that
      // was up needs no poll, since reconnecting announces the tag and catches up on its own.
      if (!connected && !stopped) {
        refresh();
      }
    }
  }

  /** Waits for {@code nanos}, or until the hold has passed if that is later. False once stopped. */
  private boolean sleep(long nanos) {
    long wait = Math.max(nanos, hold.remainingNanos());
    try {
      if (wait > 0) {
        return !stop.await(wait, TimeUnit.NANOSECONDS);
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return false;
    }
    return !stopped;
  }

  /**
   * Exponential from a second up to the poll interval, jittered; with no stream, just the interval.
   */
  Duration delay(int failures) {
    if (!streamOn) {
      return pollInterval;
    }
    long exponential = BACKOFF_BASE.toNanos() << Math.min(failures, 16);
    long ceiling = Math.min(pollInterval.toNanos(), exponential);
    return Duration.ofNanos((long) (ceiling * (0.5 + jitter.getAsDouble() / 2)));
  }

  /** The ruleset has been confirmed current as of now. Callers hold {@link #state}. */
  private void synced() {
    lastSyncAt = clock.instant();
    lastError = null;
  }

  private void markSynced() {
    synchronized (state) {
      synced();
    }
  }

  /** Records a failed attempt and tells {@code onError}, unless the failure is only the close. */
  private boolean fail(String code, String message) {
    synchronized (state) {
      if (stopped) {
        return false;
      }
      lastError = code;
    }
    report.accept(new ToggleSlashException(code, message));
    return false;
  }

  private boolean fail(String route, Http.Failure failure) {
    if (failure.kind() == Http.Kind.ABORTED) {
      return false;
    }
    if (failure.kind() == Http.Kind.TIMEOUT) {
      return fail("timeout", route + " timed out");
    }
    return fail("network", route + " failed: network error");
  }

  private void setState(StreamState next) {
    synchronized (state) {
      streamState = next;
    }
  }

  /** The whole decimal seconds of a {@code Retry-After}, or empty if it is anything else. */
  static OptionalLong retryAfterSeconds(Optional<String> header) {
    if (header.isEmpty()) {
      return OptionalLong.empty();
    }
    String text = header.get().strip();
    if (text.isEmpty()) {
      return OptionalLong.empty();
    }
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      if (c < '0' || c > '9') {
        return OptionalLong.empty();
      }
    }
    if (text.length() > 9) {
      return OptionalLong.of(Hold.MAX_SECONDS);
    }
    return OptionalLong.of(Long.parseLong(text));
  }

  private void honourRetryAfter(int status, Optional<String> header) {
    if (status == 429) {
      retryAfterSeconds(header).ifPresent(hold::extend);
    }
  }

  /** One conditional fetch of the ruleset. True if it left the ruleset confirmed current. */
  boolean refresh() {
    String route = "GET /sdk/v1/ruleset";
    Snapshot held = snapshot;
    Map<String, String> headers = new LinkedHashMap<>();
    headers.put("Authorization", "Bearer " + sdkKey);
    if (held != null && held.etag() != null) {
      headers.put("If-None-Match", held.etag());
    }
    Http.Reply reply;
    try {
      reply = http.get(baseUrl + "/sdk/v1/ruleset", headers, requestTimeout);
    } catch (Http.Failure failure) {
      return fail(route, failure);
    }
    if (reply.status() == 304) {
      markSynced();
      return true;
    }
    if (reply.status() != 200) {
      honourRetryAfter(reply.status(), reply.retryAfter());
      return fail("http_" + reply.status(), route + " answered " + reply.status());
    }
    Ruleset ruleset;
    try {
      Object body = Json.parse(reply.body());
      if (!Ruleset.isRuleset(body)) {
        return fail("bad_ruleset", route + " sent no flags and segments");
      }
      ruleset = Ruleset.of(body);
    } catch (RuntimeException e) {
      return fail("bad_ruleset", route + " sent a body that is not a ruleset");
    }
    synchronized (state) {
      snapshot = new Snapshot(ruleset, reply.etag().orElse(null));
      synced();
    }
    return true;
  }

  /**
   * Follows {@code GET /sdk/v1/stream} until it ends or fails. True if it connected, so the caller
   * can tell a stream that was up from one that never was.
   */
  private boolean follow() {
    String route = "GET /sdk/v1/stream";
    boolean connected = false;
    Http.Stream stream = null;
    setState(StreamState.CONNECTING);
    try {
      Map<String, String> headers = new LinkedHashMap<>();
      headers.put("Authorization", "Bearer " + sdkKey);
      headers.put("Accept", "text/event-stream");
      Http.Opened opened = http.open(baseUrl + "/sdk/v1/stream", headers, requestTimeout);
      if (opened.status() != 200) {
        honourRetryAfter(opened.status(), opened.retryAfter());
        fail("http_" + opened.status(), route + " answered " + opened.status());
        return false;
      }
      stream = opened.stream();
      SseParser parser = new SseParser();
      while (!stopped) {
        Object item = stream.next(streamIdle);
        if (item == null) {
          fail("timeout", route + " went quiet");
          return connected;
        }
        if (item == Http.Stream.END || item == Http.Stream.ABORTED) {
          return connected;
        }
        if (item instanceof Throwable) {
          if (!stopped) {
            fail("network", route + " failed: network error");
          }
          return connected;
        }
        byte[] chunk = (byte[]) item;
        List<SseParser.Event> events = parser.feed(chunk, chunk.length);
        for (SseParser.Event event : events) {
          if (!event.name().equals("changed") && !event.name().equals("connected")) {
            continue;
          }
          if (event.name().equals("connected")) {
            connected = true;
            synchronized (state) {
              streamState = StreamState.CONNECTED;
              lastError = null;
            }
          }
          if (!onTag(event.data())) {
            // The fetch the stream asked for failed, and a stream that stays up would never retry
            // it. End it and count it as not connected, so the backoff and the poll take over.
            return false;
          }
        }
      }
      return connected;
    } catch (Http.Failure failure) {
      fail(route, failure);
      return connected;
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return connected;
    } finally {
      if (stream != null) {
        http.release(stream);
      }
      setState(StreamState.DISCONNECTED);
    }
  }

  /** The stream names the ruleset's ETag; fetch only when it is not the one held. */
  private boolean onTag(String tag) {
    Snapshot held = snapshot;
    if (held != null && tag.equals(held.etag())) {
      markSynced();
      return true;
    }
    return refresh();
  }
}
