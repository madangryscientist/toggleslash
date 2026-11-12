package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Utf8;
import java.io.IOException;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeoutException;

/**
 * The sync thread's work, to section 7 of the SDK contract: the first load, then the loop of stream
 * and polls until {@code close}. It touches the state only through {@link Core}, holds no lock
 * during a request and calls {@code onError} with none held.
 */
final class SyncLoop {
  private static final int MAX_FAILURE_EXPONENT = 16;
  private static final int FAILURE_CAP = 64;
  private static final long ONE_SECOND = 1_000_000_000L;
  private static final String RULESET = "ruleset sync";
  private static final String STREAM = "stream";

  private final Core core;
  private final Http http;

  SyncLoop(Core core, Http http) {
    this.core = core;
    this.http = http;
  }

  /** The first ruleset request. The first load is not held by a Retry-After. */
  void firstLoad() {
    fetchRuleset();
  }

  /** Follows the server until {@code close}. */
  void run() {
    boolean streamOn = core.options.stream();
    int failures = 0;
    while (!core.closing.get()) {
      if (streamOn && !waitOutHold()) {
        return;
      }
      boolean connected = streamOn && followStream();
      if (core.closing.get()) {
        return;
      }
      failures = connected ? 0 : Math.min(failures + 1, FAILURE_CAP);
      if (streamOn) {
        core.setStream(StreamState.DISCONNECTED);
      }
      if (!pause(delayNanos(streamOn, failures))) {
        return;
      }
      if (!connected && !core.closing.get()) {
        fetchRuleset();
      }
    }
  }

  /** The wait before the next attempt, to section 7.3. */
  private long delayNanos(boolean streamOn, int failures) {
    long poll = core.options.pollNanos();
    if (!streamOn) {
      return poll;
    }
    long ceiling = Math.min(poll, ONE_SECOND << Math.min(failures, MAX_FAILURE_EXPONENT));
    double jitter = 0.5 + 0.5 * core.options.jitter().getAsDouble();
    return (long) (ceiling * jitter);
  }

  /** Waits out a Retry-After hold. False when the client closed meanwhile. */
  private boolean waitOutHold() {
    return pause(0);
  }

  /** Waits at least {@code nanos} and until the Retry-After hold has passed. False if closed. */
  private boolean pause(long nanos) {
    try {
      while (!core.closing.get()) {
        long wait = Math.max(nanos, core.rulesetHold.remainingNanos());
        if (wait <= 0) {
          return true;
        }
        core.pacer.sleep(core.closed, wait);
        nanos = 0;
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
    return false;
  }

  /**
   * One ruleset request (7.1).
   *
   * @return true if it was a sync
   */
  private boolean fetchRuleset() {
    State held = core.state.get();
    String ifNoneMatch = held.holdsEtag() ? held.etag() : null;
    Exchange exchange = null;
    try {
      long deadline = System.nanoTime() + core.options.timeoutNanos();
      exchange = http.get("/sdk/v1/ruleset", null, ifNoneMatch);
      int status = exchange.status();
      if (status == 200) {
        return load(exchange, deadline);
      }
      if (status == 304 && held.ruleset() != null) {
        core.touched();
        return true;
      }
      if (status == 429) {
        holdFor(exchange);
      }
      core.fail(LastError.http(status), RULESET);
      return false;
    } catch (IOException | TimeoutException | RuntimeException e) {
      core.fail(Core.codeOf(e), RULESET);
      return false;
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      core.fail(LastError.NETWORK, RULESET);
      return false;
    } finally {
      http.release(exchange);
    }
  }

  private boolean load(Exchange exchange, long deadline)
      throws IOException, TimeoutException, InterruptedException {
    String etag = exchange.header("ETag");
    byte[] body = exchange.readAll(deadline);
    Ruleset ruleset = parse(body);
    if (ruleset == null) {
      core.fail(LastError.BAD_RULESET, RULESET);
      return false;
    }
    core.synced(ruleset, etag == null || etag.isEmpty() ? null : etag);
    return true;
  }

  /** The ruleset a body spells, or null when it is not one (section 5, item 5). */
  private static Ruleset parse(byte[] body) {
    try {
      Object json = Json.parse(Utf8.decode(body));
      if (json instanceof Map<?, ?> map
          && map.get("flags") instanceof List<?>
          && map.get("segments") instanceof List<?>) {
        return Ruleset.create(json);
      }
      return null;
    } catch (RuntimeException e) {
      return null;
    }
  }

  private void holdFor(Exchange exchange) {
    long seconds = Hold.parseSeconds(exchange.header("Retry-After"));
    if (seconds >= 0) {
      core.rulesetHold.extend(seconds);
    }
  }

  /**
   * One stream attempt (7.2).
   *
   * @return true if the {@code connected} event arrived and a failed fetch did not end the stream
   */
  private boolean followStream() {
    core.setStream(StreamState.CONNECTING);
    Exchange exchange = null;
    boolean connected = false;
    try {
      exchange = http.get("/sdk/v1/stream", "text/event-stream", null);
      int status = exchange.status();
      if (status != 200) {
        if (status == 429) {
          holdFor(exchange);
        }
        core.fail(LastError.http(status), STREAM);
        return false;
      }
      SseParser parser = new SseParser();
      while (true) {
        byte[] chunk = exchange.next(core.options.watchdogNanos());
        if (chunk == null) {
          return connected;
        }
        for (SseParser.Event event : parser.feed(chunk)) {
          if (event.name().equals("connected")) {
            core.connected();
            connected = true;
          } else if (!event.name().equals("changed")) {
            continue;
          }
          if (!follow(event.data())) {
            return false;
          }
        }
      }
    } catch (IOException | TimeoutException | RuntimeException e) {
      core.fail(Core.codeOf(e), STREAM);
      return connected;
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      core.fail(LastError.NETWORK, STREAM);
      return connected;
    } finally {
      http.release(exchange);
    }
  }

  /** Acts on a tag the stream announced. False when the fetch it needed failed. */
  private boolean follow(String tag) {
    State held = core.state.get();
    if (held.holdsEtag() && tag.equals(held.etag())) {
      core.touched();
      return true;
    }
    return fetchRuleset();
  }
}
