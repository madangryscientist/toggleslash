package com.github.madangryscientist.toggleslash.sdks.java.service;

import java.util.List;
import java.util.Map;

/** Writes the JSON the protocol answers with. The SDK reads JSON; the service has to write it. */
final class JsonOut {
  private JsonOut() {}

  static String write(Object value) {
    StringBuilder out = new StringBuilder();
    write(value, out);
    return out.toString();
  }

  private static void write(Object value, StringBuilder out) {
    if (value == null) {
      out.append("null");
    } else if (value instanceof Boolean b) {
      out.append(b.booleanValue());
    } else if (value instanceof Double d) {
      number(d, out);
    } else if (value instanceof Number n) {
      out.append(n.longValue());
    } else if (value instanceof CharSequence s) {
      string(s, out);
    } else if (value instanceof Map<?, ?> map) {
      out.append('{');
      boolean first = true;
      for (Map.Entry<?, ?> e : map.entrySet()) {
        out.append(first ? "" : ",");
        first = false;
        string(String.valueOf(e.getKey()), out);
        out.append(':');
        write(e.getValue(), out);
      }
      out.append('}');
    } else if (value instanceof List<?> list) {
      out.append('[');
      for (int i = 0; i < list.size(); i++) {
        out.append(i == 0 ? "" : ",");
        write(list.get(i), out);
      }
      out.append(']');
    } else {
      throw new IllegalArgumentException("cannot write " + value.getClass().getName());
    }
  }

  /** A number that is not finite is written as null, as {@code JSON.stringify} does. */
  private static void number(double d, StringBuilder out) {
    if (Double.isNaN(d) || Double.isInfinite(d)) {
      out.append("null");
    } else if (d == Math.rint(d) && Math.abs(d) < 1e15) {
      out.append((long) d);
    } else {
      out.append(d);
    }
  }

  private static void string(CharSequence s, StringBuilder out) {
    out.append('"');
    for (int i = 0; i < s.length(); i++) {
      char c = s.charAt(i);
      if (c == '"' || c == '\\') {
        out.append('\\').append(c);
      } else if (c < 0x20) {
        out.append(String.format("\\u%04x", (int) c));
      } else if (Character.isHighSurrogate(c)
          && i + 1 < s.length()
          && Character.isLowSurrogate(s.charAt(i + 1))) {
        out.append(c).append(s.charAt(++i));
      } else if (Character.isSurrogate(c)) {
        // A lone half cannot be written as UTF-8; escape it as JSON allows.
        out.append(String.format("\\u%04x", (int) c));
      } else {
        out.append(c);
      }
    }
    out.append('"');
  }
}
