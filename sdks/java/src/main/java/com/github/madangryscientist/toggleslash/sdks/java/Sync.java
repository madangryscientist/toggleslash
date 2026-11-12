package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Utf8;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.net.http.HttpTimeoutException;
import java.time.Instant;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;

/**
 * Keeps one ruleset current (section 7 of the contract): the conditional load, the stream, the
 * poll, the backoff and the Retry-After hold. It runs on one daemon thread. Nothing in it throws at
 * a caller; every failure goes to {@code onError} and leaves the last good ruleset in place.
 */
final class Sync {
  private static final long BACKOFF_BASE_MILLIS = 1_000L;

  private final Options options;
  private final HttpCalls http;
  private final Clock clock;
  private final Consumer<Throwable> report;
  private final long idleMillis;

  /** Counted down by {@code close}; every wait of the SDK ends with it. */
  final CountDownLatch stop = new CountDownLatch(1);

  /** Completed once, when the first attempt has settled: true if a ruleset is held. */
  final CompletableFuture<Boolean> firstLoad = new CompletableFuture<>();

  private volatile boolean stopped;
  private volatile Snapshot snapshot;
  private volatile Status status;
  private volatile long retryAt;

  private final Object stateLock = new Object();
  // The fields below are guarded by stateLock, and only copied there.
  private Long lastSyncMillis;
  private String lastError;
  private StreamState streamState;
  private boolean closed;

  Sync(Options options, HttpCalls http, Consumer<Throwable> report) {
    this.options = options;
    this.http = http;
    this.clock = options.clock;
    this.report = report;
    this.idleMillis = options.streamIdleTimeout.toMillis();
    this.streamState = options.stream ? StreamState.CONNECTING : StreamState.OFF;
    this.status = new Status(null, null, streamState, null);
  }

  Snapshot snapshot() {
    return snapshot;
  }

  Status status() {
    return status;
  }

  /** The thread body: one load, then follow until {@link #halt()}. */
  void run() {
    try {
      refresh();
      firstLoad.complete(snapshot != null);
      int failures = 0;
      while (!stopped) {
        try {
          if (options.stream) {
            sleep(0);
          }
          if (stopped) {
            return;
          }
          boolean connected = options.stream && follow();
          failures = connected ? 0 : failures + 1;
          sleep(delay(failures));
          if (!connected && !stopped) {
            refresh();
          }
        } catch (RuntimeException e) {
          // A defect must not end the loop: say so, wait a poll interval and go on.
          fail("network", e);
          sleep(options.pollInterval.toMillis());
        }
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    } finally {
      firstLoad.complete(snapshot != null);
    }
  }

  /**
   * Stops following the server: nothing new starts and the request in flight is abandoned,
   * silently.
   */
  void halt() {
    stopped = true;
    synchronized (stateLock) {
      closed = true;
      publish();
    }
    stop.countDown();
    http.abandon();
  }

  private void sleep(long millis) throws InterruptedException {
    long wait = Math.max(millis, retryAt - clock.now());
    if (wait > 0) {
      clock.sleep(wait, stop);
    }
  }

  /** Exponential from a second up to the poll interval, jittered; with no stream, the interval. */
  private long delay(int failures) {
    long poll = options.pollInterval.toMillis();
    if (!options.stream) {
      return poll;
    }
    long ceiling = Math.min(poll, BACKOFF_BASE_MILLIS * (1L << Math.min(failures, 16)));
    return (long) (ceiling * (0.5 + options.random.getAsDouble() / 2));
  }

  /** One conditional load of the ruleset. */
  private boolean refresh() throws InterruptedException {
    try {
      Snapshot held = snapshot;
      HttpCalls.Reply reply = http.getRuleset(held == null ? null : held.etag());
      int code = reply.status();
      if (code == 304) {
        if (held == null) {
          throw new SyncException(
              "http_304", "GET /sdk/v1/ruleset answered 304 with no ruleset held");
        }
      } else if (code == 200) {
        install(parse(reply));
      } else {
        holdOff(reply);
        throw new SyncException("http_" + code, "GET /sdk/v1/ruleset answered " + code);
      }
      synced();
      return true;
    } catch (SyncException e) {
      fail(e.code(), e);
      return false;
    }
  }

  private static Snapshot parse(HttpCalls.Reply reply) throws SyncException {
    Object json;
    try {
      json = Json.parse(Utf8.decode(reply.body()));
    } catch (RuntimeException e) {
      throw new SyncException("bad_ruleset", "GET /sdk/v1/ruleset sent no JSON", e);
    }
    if (!(json instanceof Map<?, ?> map)
        || !(map.get("flags") instanceof List<?>)
        || !(map.get("segments") instanceof List<?>)) {
      throw new SyncException("bad_ruleset", "GET /sdk/v1/ruleset sent no flags and segments");
    }
    Ruleset ruleset;
    try {
      ruleset = Ruleset.parse(json);
    } catch (RuntimeException e) {
      throw new SyncException("bad_ruleset", "GET /sdk/v1/ruleset sent no usable ruleset", e);
    }
    // An empty ETag counts as none, so it is never sent back as If-None-Match.
    String etag = reply.headers().firstValue("ETag").filter(s -> !s.isEmpty()).orElse(null);
    return new Snapshot(ruleset, etag);
  }

  /**
   * Follows {@code GET /sdk/v1/stream} until it ends or fails. True if it connected (and was not
   * ended by a failed fetch), so the caller can tell a stream that was up from one that never was.
   */
  private boolean follow() throws InterruptedException {
    setStream(StreamState.CONNECTING);
    boolean[] connected = {false};
    StreamBody body = new StreamBody();
    try {
      HttpCalls.Reply reply = http.openStream(body);
      if (reply.status() != 200) {
        holdOff(reply);
        throw new SyncException(
            "http_" + reply.status(), "GET /sdk/v1/stream answered " + reply.status());
      }
      pump(body, connected);
    } catch (SyncException e) {
      fail(e.code(), e);
    } finally {
      body.cancel();
      setStream(StreamState.DISCONNECTED);
    }
    return connected[0];
  }

  /**
   * Reads events until the stream ends. The queue wait is the watchdog: a quiet stream is dropped.
   */
  private void pump(StreamBody body, boolean[] connected)
      throws SyncException, InterruptedException {
    Utf8.Decoder decoder = new Utf8.Decoder();
    SseParser parser = new SseParser();
    StringBuilder text = new StringBuilder();
    while (true) {
      Object item = body.queue.poll(idleMillis, TimeUnit.MILLISECONDS);
      if (item == null) {
        throw new SyncException("timeout", "the stream went quiet");
      }
      if (item == StreamBody.END) {
        return;
      }
      if (item instanceof StreamBody.Failure failure) {
        Throwable cause = failure.error();
        String code = cause instanceof HttpTimeoutException ? "timeout" : "network";
        throw new SyncException(code, "the stream failed", cause);
      }
      byte[] chunk = (byte[]) item;
      text.setLength(0);
      decoder.decode(chunk, 0, chunk.length, text);
      body.requestMore();
      for (SseParser.Event event : parser.feed(text)) {
        if (!event.name().equals("changed") && !event.name().equals("connected")) {
          continue;
        }
        if (event.name().equals("connected")) {
          connected[0] = true;
          streamConnected();
        }
        if (!onTag(event.data())) {
          // The fetch the stream asked for failed, and a stream that stays up would never retry it.
          // End it
          // and count it as not connected, so the backoff and the poll take over.
          connected[0] = false;
          return;
        }
      }
    }
  }

  /** The stream names the ruleset's ETag; fetch only when it is not the one held. */
  private boolean onTag(String tag) throws InterruptedException {
    Snapshot held = snapshot;
    if (held != null && held.etag() != null && tag.equals(held.etag())) {
      synced();
      return true;
    }
    return refresh();
  }

  /** Remembers what a 429 asked for; a shorter answer never shortens a longer wait. */
  private void holdOff(HttpCalls.Reply reply) {
    long wait = RetryAfter.millis(reply.status(), reply.headers());
    if (wait >= 0) {
      retryAt = Math.max(retryAt, clock.now() + wait);
    }
  }

  // ---- state ----

  private void install(Snapshot next) {
    synchronized (stateLock) {
      snapshot = next;
      publish();
    }
  }

  private void synced() {
    synchronized (stateLock) {
      lastSyncMillis = clock.now();
      lastError = null;
      publish();
    }
  }

  private void setStream(StreamState state) {
    synchronized (stateLock) {
      streamState = state;
      publish();
    }
  }

  private void streamConnected() {
    synchronized (stateLock) {
      streamState = StreamState.CONNECTED;
      lastError = null;
      publish();
    }
  }

  /** A failure that is not the abandonment of a request by {@code close}. */
  private void fail(String code, Throwable error) {
    if (stopped) {
      return;
    }
    synchronized (stateLock) {
      lastError = code;
      publish();
    }
    report.accept(error);
  }

  /** Builds the one consistent status. Called with {@code stateLock} held. */
  private void publish() {
    Snapshot held = snapshot;
    Instant at = lastSyncMillis == null ? null : Instant.ofEpochMilli(lastSyncMillis);
    status =
        new Status(
            held == null ? null : held.etag(),
            at,
            closed ? StreamState.CLOSED : streamState,
            lastError);
  }
}
