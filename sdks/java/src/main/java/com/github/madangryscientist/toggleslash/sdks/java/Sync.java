package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.time.Clock;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;
import java.util.function.DoubleSupplier;

/**
 * Keeps one ruleset current: it fetches with {@code If-None-Match}, follows the stream when it can
 * and polls when it cannot. Nothing in here throws; every failure goes to {@code onError} and
 * leaves the last good ruleset in place.
 */
final class Sync {
  private static final String RULESET = "GET /sdk/v1/ruleset";
  private static final String STREAM = "GET /sdk/v1/stream";
  private static final long BACKOFF_BASE_NANOS = TimeUnit.SECONDS.toNanos(1);

  /**
   * Everything a reader needs, swapped as one value so the ruleset and the ETag that names it never
   * part.
   */
  record View(
      Ruleset ruleset, String etag, Long lastSyncAt, StreamState stream, String lastError) {}

  private final Http http;
  private final Clock clock;
  private final DoubleSupplier jitter;
  private final boolean streaming;
  private final long pollNanos;
  private final long requestNanos;
  private final long idleNanos;
  private final Consumer<Throwable> report;
  private final Hold hold = new Hold();
  private final Wake wake = new Wake();
  private final Object lock = new Object();
  private volatile View view;
  private volatile boolean closed;

  Sync(Http http, Options options, Consumer<Throwable> report) {
    this.http = http;
    this.clock = options.clock();
    this.jitter = options.jitter();
    this.streaming = options.stream();
    this.pollNanos = options.pollNanos();
    this.requestNanos = options.requestNanos();
    this.idleNanos = options.idleNanos();
    this.report = report;
    this.view =
        new View(null, null, null, streaming ? StreamState.CONNECTING : StreamState.OFF, null);
  }

  View view() {
    return view;
  }

  /** The snapshot {@code status} answers. */
  Status status() {
    View v = view;
    return new Status(
        v.etag(), v.lastSyncAt(), closed ? StreamState.CLOSED : v.stream(), v.lastError());
  }

  boolean holding() {
    return hold.active();
  }

  /** How long a {@code Retry-After} still holds the ruleset and the stream, in nanoseconds. */
  long holdRemainingNanos() {
    return hold.remainingNanos();
  }

  /** Stops following the server: wakes every sleep and abandons every request in flight. */
  void stop() {
    closed = true;
    wake.wakeAll();
    http.abandon();
  }

  private void update(java.util.function.UnaryOperator<View> change) {
    synchronized (lock) {
      view = change.apply(view);
    }
  }

  private void setStream(StreamState state) {
    update(v -> new View(v.ruleset(), v.etag(), v.lastSyncAt(), state, v.lastError()));
  }

  private void synced() {
    long now = clock.millis();
    update(v -> new View(v.ruleset(), v.etag(), now, v.stream(), null));
  }

  private void fail(ToggleSlashException error) {
    if (closed) {
      return;
    }
    update(v -> new View(v.ruleset(), v.etag(), v.lastSyncAt(), v.stream(), error.code()));
    report.accept(error);
  }

  private void fail(Http.Failure failure, String request) {
    if (failure.kind() != Http.Kind.ABORTED) {
      fail(Errors.of(failure.kind(), request));
    }
  }

  /**
   * Loads once, then follows until {@link #stop()}. {@code firstLoad} runs when the first attempt
   * has ended.
   */
  void run(Runnable firstLoad) {
    try {
      refresh();
    } finally {
      firstLoad.run();
    }
    int failures = 0;
    while (!closed) {
      if (streaming) {
        pause(0);
      }
      if (closed) {
        return;
      }
      boolean connected = streaming && follow();
      failures = connected ? 0 : failures + 1;
      pause(delay(failures));
      if (!connected && !closed) {
        refresh();
      }
    }
  }

  /**
   * Sleeps for {@code nanos}, or until the server's {@code Retry-After} has passed if that is
   * later.
   */
  private void pause(long nanos) {
    long base = System.nanoTime() + nanos;
    for (; ; ) {
      long until = hold.until();
      long deadline = until - base > 0 ? until : base;
      wake.sleepUntil(deadline);
      if (closed) {
        return;
      }
      // A 429 that arrived during the sleep may have lengthened the hold.
      long now = System.nanoTime();
      if (deadline - now <= 0 && hold.until() - now <= 0) {
        return;
      }
    }
  }

  /**
   * Exponential from a second up to the poll interval, jittered; with no stream, just the interval.
   */
  private long delay(int failures) {
    if (!streaming) {
      return pollNanos;
    }
    long ceiling = Math.min(pollNanos, BACKOFF_BASE_NANOS << Math.min(failures, 16));
    return (long) (ceiling * (0.5 + jitter.getAsDouble() / 2));
  }

  private void honourRetryAfter(Http.Response response) {
    long seconds = Hold.parseSeconds(response.headers().firstValue("Retry-After").orElse(null));
    if (seconds >= 0) {
      hold.extend(TimeUnit.SECONDS.toNanos(seconds));
    }
  }

  /** One conditional fetch of the ruleset. */
  boolean refresh() {
    View held = view;
    String ifNoneMatch = held.ruleset() != null && held.etag() != null ? held.etag() : null;
    Http.Response response;
    try {
      response = http.getRuleset(ifNoneMatch, requestNanos);
    } catch (Http.Failure failure) {
      fail(failure, RULESET);
      return false;
    }
    int status = response.status();
    if (status == 200) {
      return load(response);
    }
    if (status == 304 && held.ruleset() != null) {
      synced();
      return true;
    }
    if (status == 429) {
      honourRetryAfter(response);
    }
    fail(Errors.status(status, RULESET));
    return false;
  }

  private boolean load(Http.Response response) {
    Ruleset ruleset;
    try {
      Object parsed = Json.parse(Utf8.decode(response.body()));
      if (!(parsed instanceof Map<?, ?> map)
          || !(map.get("flags") instanceof List<?>)
          || !(map.get("segments") instanceof List<?>)) {
        fail(new ToggleSlashException(Status.BAD_RULESET, RULESET + " sent no flags and segments"));
        return false;
      }
      ruleset = Ruleset.of(parsed);
    } catch (RuntimeException | StackOverflowError unusable) {
      fail(
          new ToggleSlashException(
              Status.BAD_RULESET, RULESET + " sent a body that is not a ruleset"));
      return false;
    }
    String etag = response.headers().firstValue("ETag").orElse("");
    long now = clock.millis();
    update(v -> new View(ruleset, etag.isEmpty() ? null : etag, now, v.stream(), null));
    return true;
  }

  /**
   * Follows {@code GET /sdk/v1/stream} until it ends or fails.
   *
   * @return true if it connected, so the caller can tell a stream that was up from one that never
   *     was
   */
  private boolean follow() {
    setStream(StreamState.CONNECTING);
    Http.Opened opened = null;
    boolean connected = false;
    try {
      opened = http.openStream(requestNanos);
      if (opened.status() != 200) {
        if (opened.status() == 429) {
          honourRetryAfter(new Http.Response(opened.status(), opened.headers(), new byte[0]));
        }
        fail(Errors.status(opened.status(), STREAM));
      } else {
        connected = pump(opened.body());
      }
    } catch (Http.Failure failure) {
      fail(failure, STREAM);
    } finally {
      if (opened != null) {
        http.release(opened.body());
      }
      if (!closed) {
        setStream(StreamState.DISCONNECTED);
      }
    }
    return connected;
  }

  /** Reads events until the stream ends; the idle bound drops one that goes quiet. */
  private boolean pump(StreamBody body) throws Http.Failure {
    Utf8 decoder = new Utf8();
    SseParser parser = new SseParser();
    boolean connected = false;
    for (; ; ) {
      byte[] bytes = body.read(idleNanos);
      if (bytes == null) {
        return connected;
      }
      StringBuilder text = new StringBuilder();
      decoder.feed(bytes, 0, bytes.length, text);
      List<SseParser.Event> events = new ArrayList<>();
      parser.feed(text, events);
      for (SseParser.Event event : events) {
        if (!event.name().equals("connected") && !event.name().equals("changed")) {
          continue;
        }
        if (event.name().equals("connected")) {
          connected = true;
          update(v -> new View(v.ruleset(), v.etag(), v.lastSyncAt(), StreamState.CONNECTED, null));
        }
        if (!onTag(event.data())) {
          // The fetch the stream asked for failed, and a stream that stays up would never retry it.
          return false;
        }
      }
    }
  }

  /** The stream names the ruleset's ETag; fetch only when it is not the one held. */
  private boolean onTag(String tag) {
    View v = view;
    if (v.ruleset() != null && v.etag() != null && tag.equals(v.etag())) {
      synced();
      return true;
    }
    return refresh();
  }
}
