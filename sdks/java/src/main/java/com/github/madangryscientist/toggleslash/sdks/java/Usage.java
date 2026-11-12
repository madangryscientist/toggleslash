package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Utf8;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.locks.ReentrantLock;
import java.util.function.Consumer;

/**
 * Counts how often each flag variation was served, per UTC day, and sends the totals in batches
 * (section 9 of the contract). {@link #record} is a map increment under a lock that is held for
 * nothing else, so it is safe on a request path. A batch that fails for a reason that can pass is
 * put back; one the server refused is dropped.
 */
final class Usage {
  /** The server accepts 1000 events and 128 KiB a request; 500 entries stay under both. */
  static final int MAX_BATCH = 500;

  private record Key(String flag, int variationIndex, String day) {}

  private final HttpCalls http;
  private final Clock clock;
  private final Consumer<Throwable> report;

  private final Object usageLock = new Object();
  private Map<Key, Long> pending = new LinkedHashMap<>();

  /** Held for the whole of a flush, so flushes are serial. Evaluation never takes it. */
  private final ReentrantLock flushLock = new ReentrantLock();

  private volatile long retryAt;

  Usage(HttpCalls http, Clock clock, Consumer<Throwable> report) {
    this.http = http;
    this.clock = clock;
    this.report = report;
  }

  /** Counts one served variation. */
  void record(String flag, int variationIndex) {
    String day =
        Instant.ofEpochMilli(clock.now()).atOffset(ZoneOffset.UTC).toLocalDate().toString();
    add(new Key(flag, variationIndex, day), 1L);
  }

  private void add(Key key, long count) {
    synchronized (usageLock) {
      pending.merge(key, count, Long::sum);
    }
  }

  /** Sends what is pending. It never throws; a call during a flush waits for that flush first. */
  void flush() {
    flushLock.lock();
    try {
      send();
    } finally {
      flushLock.unlock();
    }
  }

  /**
   * The last flush, on closing. It does not wait out a {@code Retry-After}, and does not ignore
   * one: counts still held back then are lost, and that is reported.
   */
  void finish() {
    flush();
    int left;
    synchronized (usageLock) {
      left = pending.size();
    }
    if (left > 0 && holding()) {
      report.accept(
          new IllegalStateException(
              "closed with " + left + " usage counts unsent: the server asked to wait"));
    }
  }

  private boolean holding() {
    return clock.now() < retryAt;
  }

  private void send() {
    if (holding()) {
      return;
    }
    Map<Key, Long> taken;
    synchronized (usageLock) {
      taken = pending;
      pending = new LinkedHashMap<>();
    }
    List<Map.Entry<Key, Long>> entries = new ArrayList<>(taken.entrySet());
    for (int start = 0; start < entries.size(); start += MAX_BATCH) {
      List<Map.Entry<Key, Long>> batch =
          entries.subList(start, Math.min(entries.size(), start + MAX_BATCH));
      if (!post(batch)) {
        for (Map.Entry<Key, Long> entry : entries.subList(start, entries.size())) {
          add(entry.getKey(), entry.getValue());
        }
        return;
      }
    }
  }

  /** False when the batch should be kept for another try. */
  private boolean post(List<Map.Entry<Key, Long>> batch) {
    List<Object> events = new ArrayList<>();
    for (Map.Entry<Key, Long> entry : batch) {
      Map<String, Object> event = new LinkedHashMap<>();
      event.put("flag", entry.getKey().flag());
      event.put("variationIndex", (double) entry.getKey().variationIndex());
      event.put("day", entry.getKey().day());
      event.put("count", (double) entry.getValue());
      events.add(event);
    }
    Map<String, Object> body = new LinkedHashMap<>();
    body.put("events", events);
    try {
      HttpCalls.Reply reply = http.postEvents(Utf8.encode(Json.write(body)));
      int status = reply.status();
      if (status >= 200 && status < 300) {
        return true;
      }
      long wait = RetryAfter.millis(status, reply.headers());
      if (wait >= 0) {
        retryAt = Math.max(retryAt, clock.now() + wait);
      }
      report.accept(new SyncException("http_" + status, "POST /sdk/v1/events answered " + status));
      return !retryable(status);
    } catch (SyncException e) {
      report.accept(e);
      return false;
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      report.accept(e);
      return false;
    }
  }

  /** What to do with a batch the server did not take: try it again later, or give up on it. */
  private static boolean retryable(int status) {
    return status >= 500 || status == 408 || status == 429;
  }
}
