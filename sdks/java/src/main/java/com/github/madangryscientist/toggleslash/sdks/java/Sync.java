package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.JsonException;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.io.IOException;
import java.io.InputStream;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ScheduledThreadPoolExecutor;
import java.util.concurrent.TimeUnit;

/**
 * Keeps one ruleset current: it fetches with {@code If-None-Match}, follows the stream when it can
 * and polls when it cannot. Everything here runs on the one sync thread, and nothing in it throws
 * at a caller: a failure goes to {@code onError} and leaves the last good ruleset in place.
 */
final class Sync {
  private static final long BACKOFF_BASE_MS = 1_000;

  private final Config config;
  private final Http http;
  private final Store store;
  private final Hold hold = new Hold();
  private final CountDownLatch stopped = new CountDownLatch(1);
  private final Object watchdogLock = new Object();
  private ScheduledThreadPoolExecutor watchdogPool;
  private volatile InputStream current;

  Sync(Config config, Http http, Store store) {
    this.config = config;
    this.http = http;
    this.store = store;
  }

  Hold hold() {
    return hold;
  }

  /** Loads once, calls {@code onFirstLoad}, then follows until {@link #stop()}. */
  void run(Runnable onFirstLoad) {
    try {
      refresh();
    } catch (Throwable t) {
      store.report(t);
    } finally {
      onFirstLoad.run();
    }
    int failures = 0;
    while (stopped.getCount() > 0) {
      try {
        // A 429 on the ruleset says wait, and without this the stream would be asked straight
        // after.
        if (config.stream()) {
          sleep(0);
        }
        if (stopped.getCount() == 0) {
          return;
        }
        boolean connected = config.stream() && follow();
        failures = connected ? 0 : failures + 1;
        sleep(delay(failures));
        // Sleep first: the ruleset was only just loaded, or the stream only just failed. A stream
        // that was up needs no poll, since reconnecting announces the tag and catches up.
        if (!connected && stopped.getCount() > 0) {
          refresh();
        }
      } catch (Throwable t) {
        store.report(t);
        sleep(config.pollMs());
      }
    }
  }

  /** Ends the loop, abandons what is in flight and drops the open stream. */
  void stop() {
    stopped.countDown();
    http.abandon();
    closeQuietly(current);
    synchronized (watchdogLock) {
      if (watchdogPool != null) {
        watchdogPool.shutdownNow();
      }
    }
  }

  /** Sleeps {@code ms}, or until the server's Retry-After has passed if that is later. */
  private void sleep(long ms) {
    long wait = Math.max(ms, hold.remainingMs());
    if (wait <= 0) {
      return;
    }
    try {
      stopped.await(wait, TimeUnit.MILLISECONDS);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  /** Exponential from a second up to the poll interval, jittered; with no stream, the interval. */
  private long delay(int failures) {
    if (!config.stream()) {
      return config.pollMs();
    }
    long backoff = BACKOFF_BASE_MS << Math.min(failures, 16);
    long ceiling = Math.min(config.pollMs(), backoff);
    return Math.max(1, (long) (ceiling * (0.5 + config.random().getAsDouble() / 2)));
  }

  private void fail(String code, String message, Throwable cause) {
    if (store.closing()) {
      return;
    }
    store.update(s -> s.withLastError(code));
    store.report(new ToggleSlashException(message, cause));
  }

  private void synced() {
    long now = config.clock().millis();
    store.update(s -> s.synced(now));
  }

  private static boolean hasRulesetShape(Json body) {
    return body instanceof Json.Obj o
        && o.get("flags") instanceof Json.Arr
        && o.get("segments") instanceof Json.Arr;
  }

  /** One conditional fetch of the ruleset; true when the ruleset is now confirmed current. */
  boolean refresh() {
    try {
      Store.State state = store.get();
      HttpRequest.Builder builder =
          http.request("/sdk/v1/ruleset").header("Accept", "application/json");
      String held = state.etag();
      if (state.ruleset() != null && held != null && !held.isEmpty()) {
        builder.header("If-None-Match", held);
      }
      HttpResponse<byte[]> response =
          http.send(builder.GET().build(), HttpResponse.BodyHandlers.ofByteArray(), true);
      int status = response.statusCode();
      if (status == 304 && state.ruleset() != null) {
        synced();
        return true;
      }
      if (status != 200) {
        long wait = Http.retryAfterMs(response);
        if (wait >= 0) {
          hold.extendMs(wait);
        }
        fail(
            "http_" + status,
            "GET /sdk/v1/ruleset answered "
                + status
                + (status == 304 ? " with no ruleset held" : ""),
            null);
        return false;
      }
      Ruleset loaded;
      try {
        Json body = Json.parse(response.body());
        if (!hasRulesetShape(body)) {
          fail("bad_ruleset", "GET /sdk/v1/ruleset sent no flags and segments", null);
          return false;
        }
        loaded = Ruleset.load(body);
      } catch (JsonException e) {
        fail("bad_ruleset", "GET /sdk/v1/ruleset sent a body that is not JSON", e);
        return false;
      } catch (RuntimeException e) {
        fail("bad_ruleset", "GET /sdk/v1/ruleset sent a ruleset that could not be read", e);
        return false;
      }
      String etag = response.headers().firstValue("ETag").orElse("");
      long now = config.clock().millis();
      Ruleset ruleset = loaded;
      String tag = etag.isEmpty() ? null : etag;
      store.update(s -> s.loaded(ruleset, tag, now));
      return true;
    } catch (Http.Failure f) {
      fail(f.code, "GET /sdk/v1/ruleset failed: " + f.code, f.getCause());
      return false;
    }
  }

  private ScheduledThreadPoolExecutor pool() {
    synchronized (watchdogLock) {
      if (watchdogPool == null) {
        watchdogPool =
            new ScheduledThreadPoolExecutor(1, r -> new SdkThread(r, "toggleslash-watchdog"));
        watchdogPool.setRemoveOnCancelPolicy(true);
        if (stopped.getCount() == 0) {
          watchdogPool.shutdownNow();
        }
      }
      return watchdogPool;
    }
  }

  /** One timer at a time that closes the stream when it fires; arming again replaces it. */
  private final class Watchdog {
    private final InputStream stream;
    private ScheduledFuture<?> pending;
    volatile boolean fired;

    Watchdog(InputStream stream) {
      this.stream = stream;
    }

    void arm() {
      disarm();
      try {
        pending =
            pool()
                .schedule(
                    () -> {
                      fired = true;
                      closeQuietly(stream);
                    },
                    config.idleMs(),
                    TimeUnit.MILLISECONDS);
      } catch (RuntimeException rejected) {
        // The pool is shut down: the client is closing.
      }
    }

    void disarm() {
      if (pending != null) {
        pending.cancel(false);
        pending = null;
      }
    }
  }

  private static void closeQuietly(InputStream stream) {
    if (stream == null) {
      return;
    }
    try {
      stream.close();
    } catch (IOException | RuntimeException ignored) {
      // Closing is how a read is stopped; whether it complains is of no interest.
    }
  }

  /**
   * Follows {@code GET /sdk/v1/stream} until it ends or fails. True if it connected, so the caller
   * can tell a stream that was up from one that never was.
   */
  boolean follow() {
    store.update(s -> s.withStream(StreamState.CONNECTING));
    boolean connected = false;
    InputStream in = null;
    Watchdog watchdog = null;
    try {
      HttpRequest request =
          http.request("/sdk/v1/stream").header("Accept", "text/event-stream").GET().build();
      HttpResponse<InputStream> response =
          http.send(request, HttpResponse.BodyHandlers.ofInputStream(), true);
      in = response.body();
      current = in;
      if (response.statusCode() != 200) {
        long wait = Http.retryAfterMs(response);
        if (wait >= 0) {
          hold.extendMs(wait);
        }
        fail(
            "http_" + response.statusCode(),
            "GET /sdk/v1/stream answered " + response.statusCode(),
            null);
        return false;
      }
      if (store.closing()) {
        return false;
      }
      watchdog = new Watchdog(in);
      connected = pump(in, watchdog);
    } catch (Http.Failure f) {
      fail(f.code, "GET /sdk/v1/stream failed: " + f.code, f.getCause());
    } catch (RuntimeException e) {
      fail("network", "GET /sdk/v1/stream failed", e);
    } finally {
      if (watchdog != null) {
        watchdog.disarm();
      }
      closeQuietly(in);
      current = null;
      store.update(s -> s.withStream(StreamState.DISCONNECTED));
    }
    return connected;
  }

  /** Reads events until the stream ends; the watchdog drops one that goes quiet. */
  private boolean pump(InputStream in, Watchdog watchdog) {
    Utf8.Decoder decoder = new Utf8.Decoder(true);
    SseParser parser = new SseParser();
    StringBuilder text = new StringBuilder();
    byte[] buffer = new byte[8192];
    boolean connected = false;
    while (true) {
      watchdog.arm();
      int read;
      try {
        read = in.read(buffer);
      } catch (IOException | RuntimeException e) {
        if (watchdog.fired) {
          fail("timeout", "the stream went quiet", e);
        } else {
          fail("network", "GET /sdk/v1/stream failed", e);
        }
        return connected;
      }
      if (read < 0) {
        if (watchdog.fired && !store.closing()) {
          fail("timeout", "the stream went quiet", null);
        }
        return connected;
      }
      // The refresh an event asks for has its own bound, so the idle timer rests meanwhile.
      watchdog.disarm();
      text.setLength(0);
      decoder.decode(buffer, 0, read, false, text);
      for (SseParser.Event event : parser.feed(text.toString())) {
        if (!event.name().equals("changed") && !event.name().equals("connected")) {
          continue;
        }
        if (event.name().equals("connected")) {
          connected = true;
          store.update(s -> s.withStream(StreamState.CONNECTED).withLastError(null));
        }
        if (!onTag(event.data())) {
          // The fetch the stream asked for failed, and a stream that stays up would never retry
          // it. End it and count it as not connected, so the backoff and the poll take over.
          return false;
        }
      }
    }
  }

  /** The stream names the ruleset's ETag; fetch only when it is not the one held. */
  private boolean onTag(String tag) {
    Store.State state = store.get();
    if (state.ruleset() != null && tag.equals(state.etag())) {
      synced();
      return true;
    }
    return refresh();
  }
}
