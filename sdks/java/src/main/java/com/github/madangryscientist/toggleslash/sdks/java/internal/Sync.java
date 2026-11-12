package com.github.madangryscientist.toggleslash.sdks.java.internal;

import com.github.madangryscientist.toggleslash.sdks.java.Json;
import com.github.madangryscientist.toggleslash.sdks.java.JsonException;
import com.github.madangryscientist.toggleslash.sdks.java.StreamState;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Utf8;
import java.io.IOException;
import java.io.InputStream;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ScheduledThreadPoolExecutor;
import java.util.concurrent.ThreadLocalRandom;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * The sync thread: the first load, then the loop of contract 7.3 over the ruleset request (7.1) and
 * the stream (7.2).
 */
public final class Sync {
  private final Hub hub;
  private final Thread thread;
  private final ScheduledThreadPoolExecutor timers;

  /**
   * Creates the sync loop; call {@link #start()} to run it.
   *
   * @param hub the shared state
   */
  public Sync(Hub hub) {
    this.hub = hub;
    this.thread = new SdkThread("toggleslash-sync", this::run);
    this.timers =
        new ScheduledThreadPoolExecutor(
            1,
            r -> {
              Thread t = new Thread(r, "toggleslash-watchdog");
              t.setDaemon(true);
              return t;
            });
    timers.setRemoveOnCancelPolicy(true);
  }

  /** Starts the thread. */
  public void start() {
    thread.start();
  }

  /**
   * Waits for the thread to end.
   *
   * @param millis the longest wait
   */
  public void join(long millis) {
    if (Thread.currentThread() == thread) {
      return;
    }
    try {
      thread.join(millis);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  /** Releases the watchdog thread. */
  public void release() {
    timers.shutdownNow();
  }

  private void run() {
    try {
      fetchRuleset(false);
    } catch (Throwable t) {
      failure("network", false);
    }
    hub.settleReady(false);
    long failures = 0;
    while (!hub.stop().isOpen()) {
      boolean connected = false;
      try {
        if (hub.streamOn()) {
          waitOutHold();
          if (hub.stop().isOpen()) {
            break;
          }
          connected = followStream();
        }
        failures = connected ? 0 : failures + 1;
        long wait = Math.max(delay(failures), hub.rulesHoldMillis());
        if (hub.stop().sleep(wait)) {
          break;
        }
        waitOutHold();
        if (hub.stop().isOpen()) {
          break;
        }
        if (!connected) {
          fetchRuleset(false);
        }
      } catch (Throwable t) {
        if (!hub.closing()) {
          hub.report(t);
        }
        failures++;
        if (hub.stop().sleep(Math.min(1000, hub.pollInterval().toMillis()))) {
          break;
        }
      }
    }
  }

  private void waitOutHold() {
    long hold = hub.rulesHoldMillis();
    while (hold > 0 && !hub.stop().isOpen()) {
      hub.stop().sleep(hold);
      hold = hub.rulesHoldMillis();
    }
  }

  private long delay(long failures) {
    long poll = hub.pollInterval().toMillis();
    if (!hub.streamOn()) {
      return poll;
    }
    long ceiling = Math.min(poll, 1000L << Math.min(failures, 16));
    return (long) (ceiling * (0.5 + ThreadLocalRandom.current().nextDouble() * 0.5));
  }

  /** Records a failed sync: the code goes to {@code lastError} and to {@code onError}. */
  private void failure(String code, boolean endsStream) {
    if (hub.closing()) {
      return;
    }
    hub.update(c -> {
      Core next = c.withError(code);
      return endsStream ? next.withStream(StreamState.disconnected) : next;
    });
    hub.report(new SdkException("sync failed: " + code));
  }

  /**
   * One ruleset request (7.1).
   *
   * @param fromStream whether a stream event asked for it
   * @return true if it ended as a sync
   */
  private boolean fetchRuleset(boolean fromStream) {
    Core held = hub.core();
    Hub.Reply reply;
    try {
      HttpRequest.Builder builder = hub.request("/sdk/v1/ruleset").GET();
      if (held.ruleset() != null && held.etag() != null && !held.etag().isEmpty()) {
        builder.header("If-None-Match", held.etag());
      }
      reply = hub.send(builder.build(), true);
    } catch (Hub.Failure f) {
      if (f.code() != null) {
        failure(f.code(), fromStream);
      }
      return false;
    } catch (RuntimeException e) {
      failure("network", fromStream);
      return false;
    }
    int status = reply.status();
    if (status == 200) {
      return applyBody(reply, fromStream);
    }
    if (status == 304) {
      if (held.ruleset() != null) {
        long now = hub.now();
        hub.update(c -> c.synced(now));
        return true;
      }
      failure("http_304", fromStream);
      return false;
    }
    if (status == 429) {
      holdFrom(reply);
    }
    failure("http_" + status, fromStream);
    return false;
  }

  private void holdFrom(Hub.Reply reply) {
    long seconds = Hub.retryAfter(reply.headers());
    if (seconds > 0) {
      hub.holdRules(seconds);
    }
  }

  private boolean applyBody(Hub.Reply reply, boolean fromStream) {
    Ruleset ruleset;
    try {
      Object parsed = Json.parse(Utf8.decode(reply.body()));
      if (!Ruleset.hasArrays(parsed)) {
        failure("bad_ruleset", fromStream);
        return false;
      }
      ruleset = Ruleset.create(parsed);
    } catch (JsonException e) {
      failure("bad_ruleset", fromStream);
      return false;
    }
    String etag = reply.headers().firstValue("ETag").orElse(null);
    if (etag != null && etag.isEmpty()) {
      etag = null;
    }
    String tag = etag;
    long now = hub.now();
    hub.update(c -> c.synced(ruleset, tag, now));
    return true;
  }

  /**
   * Opens the stream and follows it until it ends (7.2).
   *
   * @return true if its {@code connected} event arrived and it was not ended by a failed fetch
   */
  private boolean followStream() {
    hub.setStream(StreamState.connecting);
    InputStream body;
    CompletableFuture<HttpResponse<InputStream>> future;
    try {
      HttpRequest request =
          hub.request("/sdk/v1/stream").header("Accept", "text/event-stream").GET().build();
      future = hub.http().sendAsync(request, HttpResponse.BodyHandlers.ofInputStream());
    } catch (RuntimeException e) {
      endAttempt("network");
      return false;
    }
    Runnable abortOpen = () -> future.cancel(true);
    if (!hub.track(abortOpen)) {
      future.cancel(true);
      return false;
    }
    HttpResponse<InputStream> response;
    try {
      response = future.get(hub.requestTimeout().toNanos(), TimeUnit.NANOSECONDS);
    } catch (TimeoutException e) {
      future.cancel(true);
      endAttempt(hub.closing() ? null : "timeout");
      return false;
    } catch (ExecutionException e) {
      endAttempt(hub.closing() ? null : Hub.codeOf(e.getCause()));
      return false;
    } catch (CancellationException e) {
      endAttempt(hub.closing() ? null : "network");
      return false;
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      future.cancel(true);
      return false;
    } finally {
      hub.untrack(abortOpen);
    }
    body = response.body();
    AtomicBoolean timedOut = new AtomicBoolean();
    Runnable closer = () -> closeQuietly(body);
    if (!hub.track(closer)) {
      closeQuietly(body);
      return false;
    }
    try {
      int status = response.statusCode();
      if (body == null || status != 200) {
        if (status == 429) {
          holdFrom(new Hub.Reply(status, null, response.headers()));
        }
        endAttempt("http_" + status);
        return false;
      }
      return read(body, timedOut);
    } finally {
      hub.untrack(closer);
      closeQuietly(body);
    }
  }

  private boolean read(InputStream body, AtomicBoolean timedOut) {
    byte[] buffer = new byte[8192];
    Utf8.Decoder decoder = new Utf8.Decoder();
    SseParser parser = new SseParser();
    StringBuilder text = new StringBuilder();
    List<SseParser.Event> events = new ArrayList<>();
    boolean connected = false;
    String error = null;
    ScheduledFuture<?> timer = null;
    try {
      while (true) {
        if (timer != null) {
          timer.cancel(false);
        }
        timer =
            timers.schedule(
                () -> {
                  timedOut.set(true);
                  closeQuietly(body);
                },
                hub.tuning().watchdog.toNanos(),
                TimeUnit.NANOSECONDS);
        int n;
        try {
          n = body.read(buffer);
        } catch (IOException e) {
          error = timedOut.get() ? "timeout" : "network";
          break;
        }
        if (n < 0) {
          break;
        }
        text.setLength(0);
        decoder.decode(buffer, 0, n, text);
        events.clear();
        parser.feed(text, events);
        for (SseParser.Event event : events) {
          if (event.name().equals("connected") || event.name().equals("changed")) {
            if (event.name().equals("connected")) {
              hub.update(c -> c.withStream(StreamState.connected).withError(null));
              connected = true;
            }
            if (!onTag(event.data())) {
              connected = false;
              endAttempt(null);
              return false;
            }
          }
        }
      }
    } finally {
      if (timer != null) {
        timer.cancel(false);
      }
    }
    endAttempt(hub.closing() ? null : error);
    return connected;
  }

  /** Handles a tag from the stream; false if its fetch failed and the stream must end. */
  private boolean onTag(String tag) {
    Core held = hub.core();
    if (held.ruleset() != null
        && held.etag() != null
        && !held.etag().isEmpty()
        && held.etag().equals(tag)) {
      long now = hub.now();
      hub.update(c -> c.synced(now));
      return true;
    }
    return fetchRuleset(true);
  }

  /** Ends a stream attempt: the state and the error change together. */
  private void endAttempt(String error) {
    if (hub.closing()) {
      return;
    }
    hub.update(c -> {
      Core next = c.withStream(StreamState.disconnected);
      return error == null ? next : next.withError(error);
    });
    if (error != null) {
      hub.report(new SdkException("stream failed: " + error));
    }
  }

  private static void closeQuietly(InputStream stream) {
    if (stream == null) {
      return;
    }
    try {
      stream.close();
    } catch (IOException | RuntimeException e) {
      // closing is how a blocked read is ended; its failure changes nothing
    }
  }
}
