package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.time.Clock;
import java.time.LocalDate;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.locks.ReentrantLock;
import java.util.function.Consumer;

/**
 * Counts how often each flag variation was served, per UTC day, and sends the totals. {@link
 * #record} is a map increment under a lock that no network call holds. A batch that fails for a
 * reason that can pass is put back; one the server refused is dropped, since it would be refused
 * again.
 */
final class Usage {
  /** The server takes 1000 events and 128 KiB a request; 500 entries stay under both. */
  static final int MAX_BATCH = 500;

  private record Key(String flag, int variationIndex, String day) {}

  private final Http http;
  private final Clock clock;
  private final long requestNanos;
  private final Consumer<Throwable> report;
  private final Hold hold = new Hold();
  private final Object lock = new Object();
  private final ReentrantLock sending = new ReentrantLock();
  private Map<Key, Long> pending = new LinkedHashMap<>();

  Usage(Http http, Options options, Consumer<Throwable> report) {
    this.http = http;
    this.clock = options.clock();
    this.requestNanos = options.requestNanos();
    this.report = report;
  }

  /** Counts one served value. */
  void record(String flag, int variationIndex) {
    String day = LocalDate.ofInstant(clock.instant(), ZoneOffset.UTC).toString();
    Key key = new Key(flag, variationIndex, day);
    synchronized (lock) {
      pending.merge(key, 1L, Long::sum);
    }
  }

  /** How many distinct entries are waiting to be sent. */
  int pendingEntries() {
    synchronized (lock) {
      return pending.size();
    }
  }

  /** How long a {@code Retry-After} still holds usage, in nanoseconds. */
  long holdRemainingNanos() {
    return hold.remainingNanos();
  }

  /**
   * Sends what is pending. A call during a send waits for that send first; a hold makes it return
   * at once.
   */
  void flush() {
    sending.lock();
    try {
      send();
    } finally {
      sending.unlock();
    }
  }

  /**
   * The last flush, on closing. It will not wait out a {@code Retry-After} and will not ignore one,
   * so counts still held back then are lost, and that is reported.
   */
  void finish() {
    flush();
    int left = pendingEntries();
    if (left > 0 && hold.active()) {
      report.accept(
          new ToggleSlashException(
              "usage_lost",
              "closed with " + left + " usage counts unsent: the server asked to wait"));
    }
  }

  private void send() {
    if (hold.active()) {
      return;
    }
    Map<Key, Long> taken;
    synchronized (lock) {
      taken = pending;
      pending = new LinkedHashMap<>();
    }
    List<Map.Entry<Key, Long>> entries = new ArrayList<>(taken.entrySet());
    for (int start = 0; start < entries.size(); start += MAX_BATCH) {
      List<Map.Entry<Key, Long>> batch =
          entries.subList(start, Math.min(entries.size(), start + MAX_BATCH));
      if (!post(batch)) {
        putBack(entries.subList(start, entries.size()));
        return;
      }
    }
  }

  private void putBack(List<Map.Entry<Key, Long>> entries) {
    synchronized (lock) {
      for (Map.Entry<Key, Long> entry : entries) {
        pending.merge(entry.getKey(), entry.getValue(), Long::sum);
      }
    }
  }

  /** False when the batch should be kept for another try. */
  private boolean post(List<Map.Entry<Key, Long>> batch) {
    List<Object> events = new ArrayList<>(batch.size());
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
    Http.Response response;
    try {
      response = http.postEvents(Json.write(body), requestNanos);
    } catch (Http.Failure failure) {
      report.accept(Errors.of(failure.kind(), "POST /sdk/v1/events"));
      return false;
    }
    int status = response.status();
    if (status >= 200 && status < 300) {
      return true;
    }
    if (status == 429) {
      long seconds = Hold.parseSeconds(response.headers().firstValue("Retry-After").orElse(null));
      if (seconds >= 0) {
        hold.extend(TimeUnit.SECONDS.toNanos(seconds));
      }
    }
    report.accept(
        new ToggleSlashException("http_" + status, "POST /sdk/v1/events answered " + status));
    return !(status >= 500 || status == 408 || status == 429);
  }
}
