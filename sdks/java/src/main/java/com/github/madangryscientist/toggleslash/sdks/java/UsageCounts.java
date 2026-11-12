package com.github.madangryscientist.toggleslash.sdks.java;

import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Usage counts, summed per (flag key, variation index, UTC day) in memory (contract section 9).
 * Recording is one {@link ConcurrentHashMap#merge}; draining removes entry by entry, so a count
 * recorded while a flush drains lands either in what is sent or in the next flush, never nowhere.
 * Holds a flag key and a number; no context or end-user data ever reaches it.
 */
final class UsageCounts {

  /** One entry of the events body. */
  record Key(String flag, int variationIndex, String day) {}

  /** A key and how many times it was counted. */
  record Entry(Key key, long count) {}

  private final ConcurrentHashMap<Key, Long> counts = new ConcurrentHashMap<>();

  /** Counts one evaluation. */
  void add(String flag, int variationIndex, long wallMillis) {
    counts.merge(new Key(flag, variationIndex, day(wallMillis)), 1L, Long::sum);
  }

  /** Puts counts back after a failed request, merged with what was recorded since. */
  void putBack(List<Entry> entries) {
    for (Entry entry : entries) {
      counts.merge(entry.key(), entry.count(), Long::sum);
    }
  }

  /** The number of entries pending (one per flag, variation and day). */
  int size() {
    return counts.size();
  }

  /** Takes every pending entry out. */
  List<Entry> drain() {
    List<Entry> out = new ArrayList<>();
    for (Key key : counts.keySet()) {
      Long count = counts.remove(key);
      if (count != null) {
        out.add(new Entry(key, count));
      }
    }
    return out;
  }

  /** The UTC date of {@code wallMillis} as {@code yyyy-MM-dd}. */
  static String day(long wallMillis) {
    return Instant.ofEpochMilli(wallMillis).atOffset(ZoneOffset.UTC).toLocalDate().toString();
  }

  /** The request body: {@code {"events":[...]}} with exactly the four fields per entry. */
  static byte[] body(List<Entry> entries) {
    StringBuilder out = new StringBuilder(entries.size() * 64 + 16);
    out.append("{\"events\":[");
    boolean first = true;
    for (Entry entry : entries) {
      if (!first) {
        out.append(',');
      }
      first = false;
      out.append("{\"flag\":");
      quote(entry.key().flag(), out);
      out.append(",\"variationIndex\":").append(entry.key().variationIndex());
      out.append(",\"count\":").append(entry.count());
      out.append(",\"day\":");
      quote(entry.key().day(), out);
      out.append('}');
    }
    out.append("]}");
    return out.toString().getBytes(StandardCharsets.UTF_8);
  }

  /** A JSON string; a lone surrogate is written as an escape so the body stays valid UTF-8. */
  private static void quote(String text, StringBuilder out) {
    out.append('"');
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      if (c == '"') {
        out.append("\\\"");
      } else if (c == '\\') {
        out.append("\\\\");
      } else if (c < 0x20) {
        out.append(String.format("\\u%04x", (int) c));
      } else if (Character.isHighSurrogate(c)
          && i + 1 < text.length()
          && Character.isLowSurrogate(text.charAt(i + 1))) {
        out.append(c).append(text.charAt(i + 1));
        i++;
      } else if (Character.isSurrogate(c)) {
        out.append(String.format("\\u%04x", (int) c));
      } else {
        out.append(c);
      }
    }
    out.append('"');
  }
}
