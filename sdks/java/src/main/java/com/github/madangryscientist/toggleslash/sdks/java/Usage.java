package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.net.URI;
import java.net.http.HttpRequest;
import java.time.LocalDate;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.locks.ReentrantLock;

/** The usage counts: kept in memory, summed per flag, variation and UTC day, and flushed. */
final class Usage {
  static final int MAX_ENTRIES = 500;

  private record Key(String flag, int index, String day) {}

  private record Entry(Key key, long count) {}

  private final Object lock = new Object();
  private Map<Key, Long> counts = new LinkedHashMap<>();
  private final ReentrantLock flushLock = new ReentrantLock();
  private final Http http;
  private final String url;
  private final String sdkKey;
  private final java.time.Duration timeout;
  private final Clock clock;
  private final Reporter reporter;
  private volatile long holdUntil = Long.MIN_VALUE;

  Usage(
      Http http,
      String url,
      String sdkKey,
      java.time.Duration timeout,
      Clock clock,
      Reporter reporter) {
    this.http = http;
    this.url = url;
    this.sdkKey = sdkKey;
    this.timeout = timeout;
    this.clock = clock;
    this.reporter = reporter;
  }

  static String day(long wallMillis) {
    return LocalDate.ofEpochDay(Math.floorDiv(wallMillis, 86_400_000L)).toString();
  }

  void record(String flag, int index) {
    Key key = new Key(flag, index, day(clock.wallMillis()));
    synchronized (lock) {
      counts.merge(key, 1L, Long::sum);
    }
  }

  private List<Entry> take() {
    Map<Key, Long> taken;
    synchronized (lock) {
      taken = counts;
      counts = new LinkedHashMap<>();
    }
    List<Entry> entries = new ArrayList<>(taken.size());
    for (Map.Entry<Key, Long> entry : taken.entrySet()) {
      entries.add(new Entry(entry.getKey(), entry.getValue()));
    }
    return entries;
  }

  private void keep(List<Entry> entries) {
    synchronized (lock) {
      for (Entry entry : entries) {
        counts.merge(entry.key(), entry.count(), Long::sum);
      }
    }
  }

  private boolean held() {
    return holdUntil != Long.MIN_VALUE && clock.monoMillis() < holdUntil;
  }

  private static String body(List<Entry> entries) {
    StringBuilder out = new StringBuilder("{\"events\":[");
    boolean first = true;
    for (Entry entry : entries) {
      if (!first) {
        out.append(',');
      }
      first = false;
      out.append("{\"flag\":");
      Json.writeString(out, entry.key().flag());
      out.append(",\"variationIndex\":")
          .append(entry.key().index())
          .append(",\"count\":")
          .append(entry.count())
          .append(",\"day\":\"")
          .append(entry.key().day())
          .append("\"}");
    }
    return out.append("]}").toString();
  }

  /** Sends what is pending. The final flush does not wait out a hold. */
  void flush(boolean last) {
    flushLock.lock();
    try {
      if (!last && held()) {
        return;
      }
      List<Entry> pending = take();
      if (pending.isEmpty()) {
        return;
      }
      if (last && held()) {
        reporter.report(
            "closed with " + pending.size() + " usage counts unsent: the server asked to wait");
        return;
      }
      int sent = 0;
      while (sent < pending.size()) {
        int end = Math.min(sent + MAX_ENTRIES, pending.size());
        List<Entry> chunk = pending.subList(sent, end);
        Outcome outcome = post(chunk);
        if (outcome == Outcome.KEEP) {
          keep(pending.subList(sent, pending.size()));
          return;
        }
        sent = end;
      }
    } finally {
      flushLock.unlock();
    }
  }

  private enum Outcome {
    SENT,
    KEEP,
    DROP
  }

  private Outcome post(List<Entry> chunk) {
    byte[] bytes = Utf8.encode(body(chunk));
    HttpRequest.Builder request;
    try {
      request =
          HttpRequest.newBuilder(URI.create(url + "/sdk/v1/events"))
              .header("Authorization", "Bearer " + sdkKey)
              .header("Content-Type", "application/json")
              .header("Accept", "application/json")
              .POST(HttpRequest.BodyPublishers.ofByteArray(bytes));
    } catch (RuntimeException e) {
      reporter.report("usage request failed: " + Status.NETWORK);
      return Outcome.KEEP;
    }
    Http.Response response;
    try {
      response = http.send(request, timeout);
    } catch (Http.Failure failure) {
      reporter.report("usage request failed: " + failure.code);
      return Outcome.KEEP;
    }
    int status = response.status;
    if (status >= 200 && status < 300) {
      return Outcome.SENT;
    }
    reporter.report("usage request failed: http_" + status);
    if (status == 429) {
      long seconds = RetryAfter.seconds(response.retryAfter);
      if (seconds >= 0) {
        holdUntil = Math.max(holdUntil, clock.monoMillis() + seconds * 1000L);
      }
    }
    boolean retry = status >= 500 || status == 408 || status == 429;
    return retry ? Outcome.KEEP : Outcome.DROP;
  }
}
