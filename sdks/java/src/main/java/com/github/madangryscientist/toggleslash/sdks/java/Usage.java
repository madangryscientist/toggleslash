package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import java.time.Clock;
import java.time.Duration;
import java.time.LocalDate;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.locks.ReentrantLock;
import java.util.function.Consumer;

/**
 * Usage counts: how many times each variation of each flag was served on each UTC day. The counts
 * go out as numbers only, never a context. Recording takes a short lock that no I/O ever holds;
 * flushes run one at a time.
 */
final class Usage {
  private static final int BATCH = 500;

  private record Key(String flag, int variationIndex, String day) {}

  private final Http http;
  private final String url;
  private final String sdkKey;
  private final Duration timeout;
  private final Clock clock;
  private final Hold hold;
  private final Consumer<Throwable> report;
  private final Object lock = new Object();
  private final ReentrantLock flushLock = new ReentrantLock();
  private Map<Key, Long> pending = new LinkedHashMap<>();
  private volatile boolean recording = true;

  Usage(
      Http http,
      String baseUrl,
      String sdkKey,
      Duration timeout,
      Clock clock,
      Hold hold,
      Consumer<Throwable> report) {
    this.http = http;
    this.url = baseUrl + "/sdk/v1/events";
    this.sdkKey = sdkKey;
    this.timeout = timeout;
    this.clock = clock;
    this.hold = hold;
    this.report = report;
  }

  /** Counts one served variation, unless the client is closing. */
  void record(String flag, int variationIndex) {
    if (!recording) {
      return;
    }
    String day = LocalDate.now(clock.withZone(ZoneOffset.UTC)).toString();
    Key key = new Key(flag, variationIndex, day);
    synchronized (lock) {
      pending.merge(key, 1L, Long::sum);
    }
  }

  void stopRecording() {
    recording = false;
  }

  /** How many (flag, variation, day) entries wait to be sent. */
  int pendingEntries() {
    synchronized (lock) {
      return pending.size();
    }
  }

  /** Sends what is pending, 500 entries a request; returns at once while the server says wait. */
  void flush() {
    flushLock.lock();
    try {
      if (hold.remainingNanos() > 0) {
        return;
      }
      Map<Key, Long> taken;
      synchronized (lock) {
        taken = pending;
        pending = new LinkedHashMap<>();
      }
      List<Map.Entry<Key, Long>> entries = new ArrayList<>(taken.entrySet());
      int sent = 0;
      while (sent < entries.size()) {
        int end = Math.min(sent + BATCH, entries.size());
        boolean done = post(entries.subList(sent, end));
        if (!done) {
          // Keep this batch and the ones not tried, and stop: the next flush tries again.
          synchronized (lock) {
            for (Map.Entry<Key, Long> entry : entries.subList(sent, entries.size())) {
              pending.merge(entry.getKey(), entry.getValue(), Long::sum);
            }
          }
          return;
        }
        sent = end;
      }
    } finally {
      flushLock.unlock();
    }
  }

  /** The last flush: it does not wait out a hold; counts it could not send are reported. */
  void finish() {
    flush();
    int left = pendingEntries();
    if (left > 0 && hold.remainingNanos() > 0) {
      report.accept(
          new ToggleSlashException(
              "usage", "closed with " + left + " usage counts unsent: the server asked to wait"));
    }
  }

  /**
   * True when the batch is done with (sent, or dropped for good); false when it is worth keeping.
   */
  private boolean post(List<Map.Entry<Key, Long>> batch) {
    List<Object> events = new ArrayList<>();
    for (Map.Entry<Key, Long> entry : batch) {
      Map<String, Object> event = new LinkedHashMap<>();
      event.put("flag", entry.getKey().flag());
      event.put("variationIndex", entry.getKey().variationIndex());
      event.put("count", entry.getValue());
      event.put("day", entry.getKey().day());
      events.add(event);
    }
    Map<String, Object> body = new LinkedHashMap<>();
    body.put("events", events);
    Map<String, String> headers = new LinkedHashMap<>();
    headers.put("Authorization", "Bearer " + sdkKey);
    headers.put("Content-Type", "application/json");
    Http.Reply reply;
    try {
      reply = http.post(url, Json.write(body), headers, timeout);
    } catch (Http.Failure failure) {
      String code = failure.kind() == Http.Kind.TIMEOUT ? "timeout" : "network";
      report.accept(new ToggleSlashException(code, "POST /sdk/v1/events failed: " + code));
      return false;
    }
    int status = reply.status();
    if (status >= 200 && status < 300) {
      return true;
    }
    if (status == 429) {
      Sync.retryAfterSeconds(reply.retryAfter()).ifPresent(hold::extend);
    }
    report.accept(new ToggleSlashException("usage", "POST /sdk/v1/events answered " + status));
    boolean retryable = status >= 500 || status == 408 || status == 429;
    return !retryable;
  }
}
