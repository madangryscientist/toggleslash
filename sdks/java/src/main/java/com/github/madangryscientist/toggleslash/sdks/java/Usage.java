package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.locks.ReentrantLock;

/**
 * Counts how often each flag variation was served, per UTC day, and sends the totals in batches.
 * {@link #record} is a map merge, so it is safe on a request path. A batch that fails for a reason
 * that can pass (the server is down or busy) is put back; one the server refused is dropped, since
 * it would be refused again. After a 429 with a Retry-After nothing is sent until that has passed.
 */
final class Usage {
  /** The server takes 1000 events and 128 KiB a request; 500 entries stay under both. */
  static final int MAX_BATCH = 500;

  private record Key(String flag, int variationIndex, String day) {}

  private final Config config;
  private final Http http;
  private final Store store;
  private final Hold hold = new Hold();
  private final ConcurrentHashMap<Key, Long> pending = new ConcurrentHashMap<>();
  private final ReentrantLock sending = new ReentrantLock();
  private final CountDownLatch stopped = new CountDownLatch(1);

  Usage(Config config, Http http, Store store) {
    this.config = config;
    this.http = http;
    this.store = store;
  }

  Hold hold() {
    return hold;
  }

  int pendingEntries() {
    return pending.size();
  }

  /** Counts one served variation. Nothing is counted once the client is closing. */
  void record(String flag, int variationIndex) {
    if (store.closing()) {
      return;
    }
    String day =
        Instant.ofEpochMilli(config.clock().millis())
            .atZone(ZoneOffset.UTC)
            .toLocalDate()
            .toString();
    pending.merge(new Key(flag, variationIndex, day), 1L, Long::sum);
  }

  /** The flush loop, on its own thread: one flush each interval until {@link #stop()}. */
  void loop() {
    try {
      while (!stopped.await(config.flushMs(), TimeUnit.MILLISECONDS)) {
        try {
          flush();
        } catch (Throwable t) {
          store.report(t);
        }
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }

  void stop() {
    stopped.countDown();
  }

  /** Sends what is pending. A call during a send waits for that send first. */
  void flush() {
    sending.lock();
    try {
      send();
    } finally {
      sending.unlock();
    }
  }

  /**
   * The last flush, on closing. It will not wait out a Retry-After and will not ignore one, so
   * counts still held back are lost, and that is reported.
   */
  void finish() {
    try {
      flush();
    } catch (Throwable t) {
      store.report(t);
    }
    int left = pending.size();
    if (left > 0 && hold.remainingMs() > 0) {
      store.report(
          new ToggleSlashException(
              "closed with " + left + " usage counts unsent: the server asked to wait"));
    }
  }

  private void send() {
    if (hold.remainingMs() > 0) {
      return;
    }
    List<Map.Entry<Key, Long>> entries = new ArrayList<>();
    for (Key key : new ArrayList<>(pending.keySet())) {
      Long count = pending.remove(key);
      if (count != null) {
        entries.add(Map.entry(key, count));
      }
    }
    for (int start = 0; start < entries.size(); start += MAX_BATCH) {
      List<Map.Entry<Key, Long>> batch =
          entries.subList(start, Math.min(entries.size(), start + MAX_BATCH));
      if (!post(batch)) {
        for (Map.Entry<Key, Long> entry : entries.subList(start, entries.size())) {
          pending.merge(entry.getKey(), entry.getValue(), Long::sum);
        }
        return;
      }
    }
  }

  private static boolean retryable(int status) {
    return status >= 500 || status == 408 || status == 429;
  }

  private static byte[] body(List<Map.Entry<Key, Long>> batch) {
    List<Json> items = new ArrayList<>(batch.size());
    for (Map.Entry<Key, Long> entry : batch) {
      Map<String, Json> fields = new LinkedHashMap<>();
      fields.put("flag", new Json.Str(entry.getKey().flag()));
      fields.put("variationIndex", new Json.Num(entry.getKey().variationIndex()));
      fields.put("count", new Json.Num(entry.getValue()));
      fields.put("day", new Json.Str(entry.getKey().day()));
      items.add(new Json.Obj(fields));
    }
    return Utf8.encode(new Json.Obj(Map.of("events", new Json.Arr(items))).toJson());
  }

  /** False when the batch should be kept for another try. */
  private boolean post(List<Map.Entry<Key, Long>> batch) {
    try {
      HttpRequest request =
          http.request("/sdk/v1/events")
              .header("Content-Type", "application/json")
              .POST(HttpRequest.BodyPublishers.ofByteArray(body(batch)))
              .build();
      HttpResponse<Void> response =
          http.send(request, HttpResponse.BodyHandlers.discarding(), false);
      int status = response.statusCode();
      if (status >= 200 && status < 300) {
        return true;
      }
      long wait = Http.retryAfterMs(response);
      if (wait >= 0) {
        hold.extendMs(wait);
      }
      store.report(new ToggleSlashException("POST /sdk/v1/events answered " + status));
      return !retryable(status);
    } catch (Http.Failure f) {
      store.report(new ToggleSlashException("POST /sdk/v1/events failed: " + f.code, f.getCause()));
      return false;
    }
  }
}
