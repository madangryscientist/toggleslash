package com.github.madangryscientist.toggleslash.sdks.java.internal;

import com.github.madangryscientist.toggleslash.sdks.java.Json;
import java.net.http.HttpRequest;
import java.time.LocalDate;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.locks.ReentrantLock;

/**
 * Usage counts (contract 9): summed per flag, variation and UTC day, and sent to {@code POST
 * /sdk/v1/events} on the flush thread, then once more at {@code close}.
 */
public final class Usage {
  private static final int MAX_BATCH = 500;
  private static final long MILLIS_PER_DAY = 86_400_000L;

  private record Key(String flag, int variationIndex, long day) {}

  private record Entry(Key key, long count) {}

  private enum Outcome {
    SENT,
    DROPPED,
    KEPT
  }

  private final Hub hub;
  private final ConcurrentHashMap<Key, Long> counts = new ConcurrentHashMap<>();
  private final ReentrantLock flushLock = new ReentrantLock();
  private final Thread thread;

  /**
   * Creates the usage recorder; call {@link #start()} to run the flush thread.
   *
   * @param hub the shared state
   */
  public Usage(Hub hub) {
    this.hub = hub;
    this.thread = new SdkThread("toggleslash-flush", this::run);
  }

  /** Starts the flush thread. */
  public void start() {
    thread.start();
  }

  /**
   * Waits for the flush thread to end.
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

  /**
   * Counts one served evaluation, unless the client is closing.
   *
   * @param flag the flag key
   * @param variationIndex the variation served
   */
  public void count(String flag, int variationIndex) {
    if (hub.closing()) {
      return;
    }
    long day = Math.floorDiv(hub.now(), MILLIS_PER_DAY);
    counts.merge(new Key(flag, variationIndex, day), 1L, Long::sum);
  }

  private void run() {
    while (true) {
      long wait = Math.max(hub.flushInterval().toMillis(), hub.usageHoldMillis());
      if (hub.stop().sleep(wait)) {
        return;
      }
      try {
        flush(false);
      } catch (Throwable t) {
        if (!hub.closing()) {
          hub.report(t);
        }
      }
    }
  }

  private List<Entry> drain() {
    List<Entry> pending = new ArrayList<>();
    for (Key key : new ArrayList<>(counts.keySet())) {
      Long count = counts.remove(key);
      if (count != null) {
        pending.add(new Entry(key, count));
      }
    }
    return pending;
  }

  private void putBack(List<Entry> entries) {
    for (Entry entry : entries) {
      counts.merge(entry.key(), entry.count(), Long::sum);
    }
  }

  /**
   * Sends what is pending, serially, at most 500 entries per request.
   *
   * @param last whether this is the flush {@code close} makes
   */
  public void flush(boolean last) {
    boolean locked = false;
    try {
      if (last) {
        locked = flushLock.tryLock(hub.requestTimeout().toMillis(), TimeUnit.MILLISECONDS);
      } else {
        flushLock.lock();
        locked = true;
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
    if (!locked) {
      return;
    }
    try {
      if (hub.usageHoldMillis() > 0) {
        if (last) {
          List<Entry> lost = drain();
          if (!lost.isEmpty()) {
            hub.report(
                new SdkException(
                    "closed with "
                        + lost.size()
                        + " usage counts unsent: the server asked to wait"));
          }
        }
        return;
      }
      List<Entry> pending = drain();
      int from = 0;
      while (from < pending.size()) {
        List<Entry> batch = pending.subList(from, Math.min(from + MAX_BATCH, pending.size()));
        Outcome outcome = post(batch);
        if (outcome == Outcome.KEPT) {
          putBack(pending.subList(from, pending.size()));
          return;
        }
        from += batch.size();
      }
    } finally {
      flushLock.unlock();
    }
  }

  private Outcome post(List<Entry> batch) {
    List<Object> events = new ArrayList<>();
    for (Entry entry : batch) {
      Map<String, Object> event = new LinkedHashMap<>();
      event.put("flag", entry.key().flag());
      event.put("variationIndex", entry.key().variationIndex());
      event.put("count", entry.count());
      event.put("day", LocalDate.ofEpochDay(entry.key().day()).toString());
      events.add(event);
    }
    Map<String, Object> body = new LinkedHashMap<>();
    body.put("events", events);
    Hub.Reply reply;
    try {
      HttpRequest request =
          hub.request("/sdk/v1/events")
              .header("Content-Type", "application/json")
              .POST(
                  HttpRequest.BodyPublishers.ofByteArray(
                      Json.stringify(body).getBytes(java.nio.charset.StandardCharsets.UTF_8)))
              .build();
      reply = hub.send(request, false);
    } catch (Hub.Failure f) {
      hub.report(
          new SdkException("usage request failed: " + (f.code() == null ? "network" : f.code())));
      return Outcome.KEPT;
    } catch (RuntimeException e) {
      hub.report(new SdkException("usage request failed: network"));
      return Outcome.KEPT;
    }
    int status = reply.status();
    if (status >= 200 && status < 300) {
      return Outcome.SENT;
    }
    hub.report(new SdkException("usage request failed: http_" + status));
    if (status == 429) {
      long seconds = Hub.retryAfter(reply.headers());
      if (seconds > 0) {
        hub.holdUsage(seconds);
      }
      return Outcome.KEPT;
    }
    if (status >= 500 || status == 408) {
      return Outcome.KEPT;
    }
    return Outcome.DROPPED;
  }
}
