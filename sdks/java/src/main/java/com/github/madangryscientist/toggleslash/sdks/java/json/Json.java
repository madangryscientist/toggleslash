package com.github.madangryscientist.toggleslash.sdks.java.json;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A JSON parser and writer over plain Java values: {@code Map<String,Object>}, {@code
 * List<Object>}, {@code Double}, {@code String}, {@code Boolean} and {@code null}.
 *
 * <p>Every number is a binary64 {@code Double}, so {@code 1e999} is infinity and {@code
 * 9007199254740993} reads as {@code 9007199254740992}. A duplicate key keeps the last value, a
 * leading byte order mark is skipped, and a lone surrogate written as an escape stays a lone
 * surrogate in the string.
 */
public final class Json {
  /** The deepest nesting the parser accepts; the contract requires at least 512. */
  public static final int MAX_DEPTH = 1000;

  private final String text;
  private int pos;

  private Json(String text) {
    this.text = text;
  }

  /**
   * Parses a JSON text.
   *
   * @param text the text
   * @return the value
   * @throws JsonException if the text is not one JSON value, or nests too deeply
   */
  public static Object parse(String text) {
    Json parser = new Json(text);
    if (!text.isEmpty() && text.charAt(0) == '\uFEFF') {
      parser.pos = 1;
    }
    parser.skipSpace();
    Object value = parser.value(0);
    parser.skipSpace();
    if (parser.pos != text.length()) {
      throw parser.fail("unexpected content after the value");
    }
    return value;
  }

  private JsonException fail(String what) {
    return new JsonException(what + " at offset " + pos);
  }

  private void skipSpace() {
    while (pos < text.length()) {
      char c = text.charAt(pos);
      if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
        pos++;
      } else {
        return;
      }
    }
  }

  private Object value(int depth) {
    if (pos >= text.length()) {
      throw fail("unexpected end");
    }
    char c = text.charAt(pos);
    switch (c) {
      case '{':
        return object(depth + 1);
      case '[':
        return array(depth + 1);
      case '"':
        return string();
      case 't':
        literal("true");
        return Boolean.TRUE;
      case 'f':
        literal("false");
        return Boolean.FALSE;
      case 'n':
        literal("null");
        return null;
      default:
        if (c == '-' || (c >= '0' && c <= '9')) {
          return number();
        }
        throw fail("unexpected character");
    }
  }

  private void literal(String word) {
    if (!text.startsWith(word, pos)) {
      throw fail("bad literal");
    }
    pos += word.length();
  }

  private Map<String, Object> object(int depth) {
    if (depth > MAX_DEPTH) {
      throw fail("nested too deeply");
    }
    pos++;
    Map<String, Object> map = new LinkedHashMap<>();
    skipSpace();
    if (pos < text.length() && text.charAt(pos) == '}') {
      pos++;
      return map;
    }
    while (true) {
      skipSpace();
      if (pos >= text.length() || text.charAt(pos) != '"') {
        throw fail("expected a key");
      }
      String key = string();
      skipSpace();
      if (pos >= text.length() || text.charAt(pos) != ':') {
        throw fail("expected a colon");
      }
      pos++;
      skipSpace();
      map.put(key, value(depth));
      skipSpace();
      if (pos >= text.length()) {
        throw fail("unexpected end");
      }
      char c = text.charAt(pos++);
      if (c == '}') {
        return map;
      }
      if (c != ',') {
        pos--;
        throw fail("expected a comma or a closing brace");
      }
    }
  }

  private List<Object> array(int depth) {
    if (depth > MAX_DEPTH) {
      throw fail("nested too deeply");
    }
    pos++;
    List<Object> list = new ArrayList<>();
    skipSpace();
    if (pos < text.length() && text.charAt(pos) == ']') {
      pos++;
      return list;
    }
    while (true) {
      skipSpace();
      list.add(value(depth));
      skipSpace();
      if (pos >= text.length()) {
        throw fail("unexpected end");
      }
      char c = text.charAt(pos++);
      if (c == ']') {
        return list;
      }
      if (c != ',') {
        pos--;
        throw fail("expected a comma or a closing bracket");
      }
    }
  }

  private String string() {
    pos++;
    StringBuilder sb = null;
    int start = pos;
    while (true) {
      if (pos >= text.length()) {
        throw fail("unterminated string");
      }
      char c = text.charAt(pos);
      if (c == '"') {
        String result =
            sb == null ? text.substring(start, pos) : sb.append(text, start, pos).toString();
        pos++;
        return result;
      }
      if (c < 0x20) {
        throw fail("control character in a string");
      }
      if (c == '\\') {
        if (sb == null) {
          sb = new StringBuilder();
        }
        sb.append(text, start, pos);
        pos++;
        sb.append(escape());
        start = pos;
      } else {
        pos++;
      }
    }
  }

  private char escape() {
    if (pos >= text.length()) {
      throw fail("unterminated escape");
    }
    char c = text.charAt(pos++);
    switch (c) {
      case '"':
      case '\\':
      case '/':
        return c;
      case 'b':
        return '\b';
      case 'f':
        return '\f';
      case 'n':
        return '\n';
      case 'r':
        return '\r';
      case 't':
        return '\t';
      case 'u':
        return unicode();
      default:
        pos--;
        throw fail("bad escape");
    }
  }

  private char unicode() {
    if (pos + 4 > text.length()) {
      throw fail("bad unicode escape");
    }
    int v = 0;
    for (int i = 0; i < 4; i++) {
      int d = Character.digit(text.charAt(pos + i), 16);
      char ch = text.charAt(pos + i);
      boolean ascii =
          (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F');
      if (d < 0 || !ascii) {
        throw fail("bad unicode escape");
      }
      v = v * 16 + d;
    }
    pos += 4;
    return (char) v;
  }

  private Double number() {
    int start = pos;
    if (text.charAt(pos) == '-') {
      pos++;
    }
    if (pos >= text.length()) {
      throw fail("bad number");
    }
    char c = text.charAt(pos);
    if (c == '0') {
      pos++;
    } else if (c >= '1' && c <= '9') {
      digits();
    } else {
      throw fail("bad number");
    }
    if (pos < text.length() && text.charAt(pos) == '.') {
      pos++;
      if (digits() == 0) {
        throw fail("bad number");
      }
    }
    if (pos < text.length() && (text.charAt(pos) == 'e' || text.charAt(pos) == 'E')) {
      pos++;
      if (pos < text.length() && (text.charAt(pos) == '+' || text.charAt(pos) == '-')) {
        pos++;
      }
      if (digits() == 0) {
        throw fail("bad number");
      }
    }
    return Double.parseDouble(text.substring(start, pos));
  }

  private int digits() {
    int n = 0;
    while (pos < text.length() && text.charAt(pos) >= '0' && text.charAt(pos) <= '9') {
      pos++;
      n++;
    }
    return n;
  }

  /**
   * Writes a value as JSON. A number that is not finite is written as {@code null}, as {@code
   * JSON.stringify} does, and a lone surrogate is written as an escape so no encoder can change it.
   *
   * @param value a map, list, string, boolean, number or {@code null}
   * @return the JSON text
   * @throws IllegalArgumentException for any other type
   */
  public static String write(Object value) {
    StringBuilder sb = new StringBuilder();
    write(value, sb);
    return sb.toString();
  }

  private static void write(Object value, StringBuilder sb) {
    if (value == null) {
      sb.append("null");
    } else if (value instanceof String s) {
      writeString(s, sb);
    } else if (value instanceof Boolean b) {
      sb.append(b.booleanValue());
    } else if (value instanceof Double || value instanceof Float) {
      writeDouble(((Number) value).doubleValue(), sb);
    } else if (value instanceof Number n) {
      sb.append(n.longValue() == n.doubleValue() ? Long.toString(n.longValue()) : n.toString());
    } else if (value instanceof Map<?, ?> m) {
      sb.append('{');
      boolean first = true;
      for (Map.Entry<?, ?> e : m.entrySet()) {
        if (!first) {
          sb.append(',');
        }
        first = false;
        writeString((String) e.getKey(), sb);
        sb.append(':');
        write(e.getValue(), sb);
      }
      sb.append('}');
    } else if (value instanceof List<?> l) {
      sb.append('[');
      for (int i = 0; i < l.size(); i++) {
        if (i > 0) {
          sb.append(',');
        }
        write(l.get(i), sb);
      }
      sb.append(']');
    } else {
      throw new IllegalArgumentException("not a JSON value: " + value.getClass().getName());
    }
  }

  private static void writeDouble(double d, StringBuilder sb) {
    if (Double.isNaN(d) || Double.isInfinite(d)) {
      sb.append("null");
    } else if (d == Math.rint(d) && Math.abs(d) < 1e15) {
      if (d == 0 && 1 / d < 0) {
        sb.append("-0");
      } else {
        sb.append((long) d);
      }
    } else {
      sb.append(Double.toString(d));
    }
  }

  private static void writeString(String s, StringBuilder sb) {
    sb.append('"');
    int n = s.length();
    for (int i = 0; i < n; i++) {
      char c = s.charAt(i);
      switch (c) {
        case '"':
          sb.append("\\\"");
          break;
        case '\\':
          sb.append("\\\\");
          break;
        case '\n':
          sb.append("\\n");
          break;
        case '\r':
          sb.append("\\r");
          break;
        case '\t':
          sb.append("\\t");
          break;
        default:
          if (c < 0x20) {
            sb.append(String.format("\\u%04x", (int) c));
          } else if (Character.isHighSurrogate(c)
              && i + 1 < n
              && Character.isLowSurrogate(s.charAt(i + 1))) {
            sb.append(c).append(s.charAt(++i));
          } else if (Character.isSurrogate(c)) {
            sb.append(String.format("\\u%04x", (int) c));
          } else {
            sb.append(c);
          }
      }
    }
    sb.append('"');
  }
}
