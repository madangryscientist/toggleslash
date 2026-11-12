package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * The in-memory usage counts (contract section 9): per flag, variation index and UTC day. Recording
 * is a short critical section and never waits on the network; the flush takes the whole map out and
 * puts back what it could not send.
 */
final class Usage {
  /** One count to send. */
  record Entry(String flag, int variationIndex, String day, long count) {}

  private record Key(String flag, int variationIndex, String day) {}

  private final Object lock = new Object();
  private Map<Key, Long> counts = new LinkedHashMap<>();
  private boolean closed;

  /** The UTC date of an instant as {@code yyyy-MM-dd}. */
  static String day(Instant now) {
    return LocalDate.ofInstant(now, ZoneOffset.UTC).toString();
  }

  /** Adds one, unless the client is closed. */
  void record(String flag, int variationIndex, Instant now) {
    Key key = new Key(flag, variationIndex, day(now));
    synchronized (lock) {
      if (!closed) {
        counts.merge(key, 1L, Long::sum);
      }
    }
  }

  /** Stops counting; what is pending stays. */
  void close() {
    synchronized (lock) {
      closed = true;
    }
  }

  /** Takes every pending count out. */
  List<Entry> take() {
    Map<Key, Long> taken;
    synchronized (lock) {
      taken = counts;
      counts = new LinkedHashMap<>();
    }
    List<Entry> entries = new ArrayList<>(taken.size());
    for (Map.Entry<Key, Long> e : taken.entrySet()) {
      Key k = e.getKey();
      entries.add(new Entry(k.flag(), k.variationIndex(), k.day(), e.getValue()));
    }
    return entries;
  }

  /** Puts counts back, added to whatever was recorded since. */
  void restore(List<Entry> entries) {
    synchronized (lock) {
      for (Entry e : entries) {
        counts.merge(new Key(e.flag(), e.variationIndex(), e.day()), e.count(), Long::sum);
      }
    }
  }

  /** Throws away every pending count and says how many entries that was. */
  int discard() {
    synchronized (lock) {
      int n = counts.size();
      counts = new LinkedHashMap<>();
      return n;
    }
  }

  /** How many entries are pending. */
  int pending() {
    synchronized (lock) {
      return counts.size();
    }
  }

  /** The JSON body of one events request. */
  static String body(List<Entry> chunk) {
    List<Object> events = new ArrayList<>(chunk.size());
    for (Entry e : chunk) {
      Map<String, Object> event = new LinkedHashMap<>();
      event.put("flag", e.flag());
      event.put("variationIndex", e.variationIndex());
      event.put("count", e.count());
      event.put("day", e.day());
      events.add(event);
    }
    Map<String, Object> root = new LinkedHashMap<>();
    root.put("events", events);
    return Json.write(root);
  }
}
