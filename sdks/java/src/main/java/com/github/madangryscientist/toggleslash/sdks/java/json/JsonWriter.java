package com.github.madangryscientist.toggleslash.sdks.java.json;

import java.util.Map;

/** Writes a {@link Json} value as JSON text. */
final class JsonWriter {
  private static final char[] HEX = "0123456789abcdef".toCharArray();

  private JsonWriter() {}

  static void write(Json value, StringBuilder out) {
    if (value instanceof Json.Null) {
      out.append("null");
    } else if (value instanceof Json.Bool b) {
      out.append(b.value() ? "true" : "false");
    } else if (value instanceof Json.Num n) {
      number(n.value(), out);
    } else if (value instanceof Json.Str s) {
      string(s.value(), out);
    } else if (value instanceof Json.Arr a) {
      out.append('[');
      boolean first = true;
      for (Json item : a.items()) {
        if (!first) {
          out.append(',');
        }
        first = false;
        write(item, out);
      }
      out.append(']');
    } else if (value instanceof Json.Obj o) {
      out.append('{');
      boolean first = true;
      for (Map.Entry<String, Json> entry : o.fields().entrySet()) {
        if (!first) {
          out.append(',');
        }
        first = false;
        string(entry.getKey(), out);
        out.append(':');
        write(entry.getValue(), out);
      }
      out.append('}');
    }
  }

  private static void number(double v, StringBuilder out) {
    if (Double.isNaN(v)) {
      out.append("null");
    } else if (Double.isInfinite(v)) {
      out.append(v > 0 ? "1e999" : "-1e999");
    } else if (v == Math.rint(v) && Math.abs(v) < 1e15) {
      out.append((long) v);
    } else {
      out.append(v);
    }
  }

  static void string(String s, StringBuilder out) {
    out.append('"');
    for (int i = 0; i < s.length(); i++) {
      char c = s.charAt(i);
      if (c == '"') {
        out.append("\\\"");
      } else if (c == '\\') {
        out.append("\\\\");
      } else if (c == '\n') {
        out.append("\\n");
      } else if (c == '\r') {
        out.append("\\r");
      } else if (c == '\t') {
        out.append("\\t");
      } else if (c < 0x20 || c == 0x7F || Character.isSurrogate(c)) {
        if (Character.isHighSurrogate(c)
            && i + 1 < s.length()
            && Character.isLowSurrogate(s.charAt(i + 1))) {
          out.append(c).append(s.charAt(++i));
        } else {
          escape(c, out);
        }
      } else {
        out.append(c);
      }
    }
    out.append('"');
  }

  private static void escape(char c, StringBuilder out) {
    out.append("\\u")
        .append(HEX[(c >> 12) & 15])
        .append(HEX[(c >> 8) & 15])
        .append(HEX[(c >> 4) & 15])
        .append(HEX[c & 15]);
  }
}
