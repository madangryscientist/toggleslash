package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.locks.ReentrantLock;

/**
 * Counts how often each flag variation was served, per UTC day, and sends the totals in batches.
 * {@link #record} is a map increment under a lock that no I/O ever holds, so it is safe on a
 * request path. A batch that fails for a reason that can pass (the server is down or busy) is put
 * back; one the server refused is dropped, since it would be refused again. After a 429 with a
 * {@code Retry-After}, nothing is sent until that has passed. The counts carry a flag key, a
 * variation index and a day: never a context.
 */
final class Usage {
  /** The server takes 1000 events and 128 KiB a request; an entry is about 140 bytes at most. */
  private static final int MAX_BATCH = 500;

  private record Key(String flag, int variationIndex, String day) {}

  private final String base;
  private final String sdkKey;
  private final Http http;
  private final Clock clock;
  private final Reporter reporter;
  private final long timeoutMs;
  private final long capMs;
  private final Hold hold = new Hold();

  private final Object pendingLock = new Object();
  private Map<Key, long[]> pending = new LinkedHashMap<>();
  private boolean counting = true;

  /**
   * Held for the length of one flush, so flushes are serial. It is never held during a callback.
   */
  private final ReentrantLock sending = new ReentrantLock();

  Usage(
      String base,
      String sdkKey,
      Http http,
      Clock clock,
      Reporter reporter,
      long timeoutMs,
      long capMs) {
    this.base = base;
    this.sdkKey = sdkKey;
    this.http = http;
    this.clock = clock;
    this.reporter = reporter;
    this.timeoutMs = timeoutMs;
    this.capMs = capMs;
  }

  static String utcDay(long epochMs) {
    return Instant.ofEpochMilli(epochMs).atZone(ZoneOffset.UTC).toLocalDate().toString();
  }

  void record(String flag, int variationIndex) {
    String day = utcDay(clock.now());
    synchronized (pendingLock) {
      if (counting) {
        add(pending, new Key(flag, variationIndex, day), 1);
      }
    }
  }

  /** Nothing is counted from here on. */
  void stopCounting() {
    synchronized (pendingLock) {
      counting = false;
    }
  }

  private static void add(Map<Key, long[]> into, Key key, long count) {
    long[] existing = into.get(key);
    if (existing == null) {
      into.put(key, new long[] {count});
    } else {
      existing[0] += count;
    }
  }

  private boolean holding() {
    return hold.remaining(clock.now()) > 0;
  }

  /** Sends what is pending. It never throws; a call during a send waits for that send first. */
  void flush() {
    sending.lock();
    try {
      send();
    } finally {
      sending.unlock();
    }
  }

  /**
   * The last flush, on closing. It will not wait out a {@code Retry-After}, and it will not ignore
   * one, so counts still held back then are lost, and that is reported.
   */
  void finish() {
    flush();
    int left;
    synchronized (pendingLock) {
      left = pending.size();
    }
    if (left > 0 && holding()) {
      reporter.report(
          new ToggleSlashException(
              null,
              "closed with " + left + " usage counts unsent: the server asked to wait",
              null));
    }
  }

  private void send() {
    if (holding()) {
      return;
    }
    List<Map.Entry<Key, long[]>> entries;
    synchronized (pendingLock) {
      entries = new ArrayList<>(pending.entrySet());
      pending = new LinkedHashMap<>();
    }
    for (int start = 0; start < entries.size(); start += MAX_BATCH) {
      List<Map.Entry<Key, long[]>> batch =
          entries.subList(start, Math.min(start + MAX_BATCH, entries.size()));
      if (!post(batch)) {
        synchronized (pendingLock) {
          Map<Key, long[]> merged = new LinkedHashMap<>();
          for (Map.Entry<Key, long[]> entry : entries.subList(start, entries.size())) {
            add(merged, entry.getKey(), entry.getValue()[0]);
          }
          for (Map.Entry<Key, long[]> entry : pending.entrySet()) {
            add(merged, entry.getKey(), entry.getValue()[0]);
          }
          pending = merged;
        }
        return;
      }
    }
  }

  private static boolean retryable(int status) {
    return status >= 500 || status == 408 || status == 429;
  }

  private static byte[] bodyOf(List<Map.Entry<Key, long[]>> batch) {
    List<Object> events = new ArrayList<>();
    for (Map.Entry<Key, long[]> entry : batch) {
      Map<String, Object> event = new LinkedHashMap<>();
      event.put("flag", entry.getKey().flag());
      event.put("variationIndex", (double) entry.getKey().variationIndex());
      event.put("count", (double) entry.getValue()[0]);
      event.put("day", entry.getKey().day());
      events.add(event);
    }
    Map<String, Object> body = new LinkedHashMap<>();
    body.put("events", events);
    return Utf8.encode(Json.stringify(body));
  }

  /** False when the batch should be kept for another try. */
  private boolean post(List<Map.Entry<Key, long[]>> batch) {
    try {
      Map<String, String> headers = new LinkedHashMap<>();
      headers.put("Authorization", "Bearer " + sdkKey);
      headers.put("Content-Type", "application/json");
      Http.Answer answer =
          http.request("POST", base + "/sdk/v1/events", headers, bodyOf(batch), timeoutMs, false);
      int status = answer.status();
      if (status >= 200 && status < 300) {
        return true;
      }
      if (status == 429) {
        long wait = Hold.parse(answer.header("Retry-After"), capMs);
        if (wait >= 0) {
          hold.extend(clock.now() + wait);
        }
      }
      reporter.report(
          new ToggleSlashException(
              "http_" + status, "POST /sdk/v1/events answered " + status, null));
      return !retryable(status);
    } catch (ToggleSlashException e) {
      reporter.report(e);
      return false;
    } catch (RuntimeException e) {
      reporter.report(new ToggleSlashException("network", "POST /sdk/v1/events failed", e));
      return false;
    }
  }
}
