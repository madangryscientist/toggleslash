package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Utf8;
import java.io.IOException;
import java.time.LocalDate;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.locks.ReentrantLock;

/**
 * Usage counts, to section 9 of the SDK contract: summed per flag, variation index and UTC day,
 * sent in batches of at most 500 entries. Recording takes {@link #lock} for one map add; the flush
 * lock is held across requests but is never taken by an evaluation.
 */
final class Usage {
  /** The most entries one request carries. */
  static final int BATCH = 500;

  private record Key(String flag, int variationIndex, String day) {}

  private record Entry(Key key, long count) {}

  private enum Outcome {
    SENT,
    KEPT,
    DROPPED
  }

  private final Core core;
  private final Http http;
  private final Object lock = new Object();
  private final ReentrantLock flushLock = new ReentrantLock();
  private Map<Key, Long> pending = new LinkedHashMap<>();
  private boolean stopped;

  Usage(Core core, Http http) {
    this.core = core;
    this.http = http;
  }

  /**
   * Counts one clean serve of a flag.
   *
   * @param flag the flag key
   * @param variationIndex the served variation
   */
  void record(String flag, int variationIndex) {
    String day = LocalDate.ofInstant(core.options.clock().instant(), ZoneOffset.UTC).toString();
    Key key = new Key(flag, variationIndex, day);
    synchronized (lock) {
      if (!stopped) {
        pending.merge(key, 1L, Long::sum);
      }
    }
  }

  /** Nothing is counted from now on. */
  void stop() {
    synchronized (lock) {
      stopped = true;
    }
  }

  private List<Entry> take() {
    Map<Key, Long> taken;
    synchronized (lock) {
      taken = pending;
      pending = new LinkedHashMap<>();
    }
    List<Entry> entries = new ArrayList<>(taken.size());
    for (Map.Entry<Key, Long> e : taken.entrySet()) {
      entries.add(new Entry(e.getKey(), e.getValue()));
    }
    return entries;
  }

  private void putBack(List<Entry> entries) {
    synchronized (lock) {
      for (Entry e : entries) {
        pending.merge(e.key(), e.count(), Long::sum);
      }
    }
  }

  private int pendingSize() {
    synchronized (lock) {
      return pending.size();
    }
  }

  /**
   * Sends what is pending. A flush that starts while another runs waits for it.
   *
   * @param last true for the final flush at {@code close}: it does not wait out a hold, it reports
   *     the counts a hold makes it lose, and it is not cut short by the closing flag
   */
  void flush(boolean last) {
    flushLock.lock();
    try {
      if (last) {
        if (core.usageHold.remainingNanos() > 0) {
          int lost = pendingSize();
          if (lost > 0) {
            core.report(
                new ToggleSlashException(
                    "closed with " + lost + " usage counts unsent: the server asked to wait",
                    "usage_unsent"));
          }
          return;
        }
      } else if (core.closing.get() || core.usageHold.remainingNanos() > 0) {
        return;
      }
      List<Entry> entries = take();
      for (int from = 0; from < entries.size(); from += BATCH) {
        if (!last && core.closing.get()) {
          putBack(entries.subList(from, entries.size()));
          return;
        }
        List<Entry> batch = entries.subList(from, Math.min(from + BATCH, entries.size()));
        Outcome outcome = send(batch);
        if (outcome == Outcome.KEPT) {
          putBack(entries.subList(from, entries.size()));
          return;
        }
      }
    } finally {
      flushLock.unlock();
    }
  }

  private Outcome send(List<Entry> batch) {
    Exchange exchange = null;
    try {
      exchange = http.post("/sdk/v1/events", body(batch));
      int status = exchange.status();
      if (status >= 200 && status < 300) {
        return Outcome.SENT;
      }
      if (status == 429) {
        long seconds = Hold.parseSeconds(exchange.header("Retry-After"));
        if (seconds >= 0) {
          core.usageHold.extend(seconds);
        }
        return failed(LastError.http(status), Outcome.KEPT);
      }
      if (status >= 500 || status == 408) {
        return failed(LastError.http(status), Outcome.KEPT);
      }
      return failed(LastError.http(status), Outcome.DROPPED);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      return failed(LastError.NETWORK, Outcome.KEPT);
    } catch (IOException | TimeoutException | RuntimeException e) {
      return failed(Core.codeOf(e), Outcome.KEPT);
    } finally {
      http.release(exchange);
    }
  }

  private Outcome failed(String code, Outcome outcome) {
    core.report(new ToggleSlashException("usage request failed: " + code, code));
    return outcome;
  }

  /** The request body: exactly flag, variationIndex, count and day for each entry. */
  private static byte[] body(List<Entry> batch) {
    StringBuilder json = new StringBuilder(64 + batch.size() * 80).append("{\"events\":[");
    boolean first = true;
    for (Entry e : batch) {
      if (!first) {
        json.append(',');
      }
      first = false;
      json.append("{\"flag\":");
      quote(json, e.key().flag());
      json.append(",\"variationIndex\":").append(e.key().variationIndex());
      json.append(",\"count\":").append(e.count());
      json.append(",\"day\":\"").append(e.key().day()).append("\"}");
    }
    json.append("]}");
    return Utf8.encode(json.toString());
  }

  private static void quote(StringBuilder out, String text) {
    out.append('"');
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      switch (c) {
        case '"' -> out.append("\\\"");
        case '\\' -> out.append("\\\\");
        case '\n' -> out.append("\\n");
        case '\r' -> out.append("\\r");
        case '\t' -> out.append("\\t");
        default -> {
          if (c < 0x20) {
            out.append(String.format("\\u%04x", (int) c));
          } else {
            out.append(c);
          }
        }
      }
    }
    out.append('"');
  }
}
