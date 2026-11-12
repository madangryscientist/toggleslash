package com.github.madangryscientist.toggleslash.sdks.java.json;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A small JSON parser and writer. Values are plain Java objects: {@code null}, {@link Boolean},
 * {@link Double} (every number is a binary64 value), {@link String}, {@link List} and {@link Map}.
 * A duplicate key keeps the last value, a leading byte order mark is skipped by {@link
 * #parse(byte[], int, int)}, and a lone surrogate written as {@code \}{@code uD800} stays in the
 * string.
 */
public final class Json {
  /** The deepest nesting {@link #parse(String)} accepts. */
  public static final int MAX_DEPTH = 1000;

  private final String text;
  private int pos;

  private Json(String text) {
    this.text = text;
  }

  /**
   * Parses UTF-8 bytes as the SDK reads a response body: invalid sequences are replaced as the
   * WHATWG Encoding standard says, and a byte order mark at the start is skipped.
   *
   * @param bytes the buffer to read from
   * @param offset the first byte to read
   * @param length how many bytes to read
   * @return the value
   * @throws JsonException when the text is not valid JSON
   */
  public static Object parse(byte[] bytes, int offset, int length) {
    String decoded = Utf8.decode(bytes, offset, length);
    return parse(decoded.startsWith("﻿") ? decoded.substring(1) : decoded);
  }

  /**
   * Parses JSON text.
   *
   * @param text the JSON text
   * @return the value
   * @throws JsonException when the text is not valid JSON
   */
  public static Object parse(String text) {
    Json parser = new Json(text);
    parser.skipSpace();
    Object value = parser.value(0);
    parser.skipSpace();
    if (parser.pos != text.length()) {
      throw new JsonException("unexpected text after the value");
    }
    return value;
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
      throw new JsonException("unexpected end of text");
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
        throw new JsonException("unexpected character");
    }
  }

  private void literal(String word) {
    if (!text.startsWith(word, pos)) {
      throw new JsonException("unexpected character");
    }
    pos += word.length();
  }

  private Map<String, Object> object(int depth) {
    if (depth > MAX_DEPTH) {
      throw new JsonException("nesting is too deep");
    }
    pos++;
    Map<String, Object> out = new LinkedHashMap<>();
    skipSpace();
    if (pos < text.length() && text.charAt(pos) == '}') {
      pos++;
      return out;
    }
    while (true) {
      skipSpace();
      if (pos >= text.length() || text.charAt(pos) != '"') {
        throw new JsonException("expected a string key");
      }
      String key = string();
      skipSpace();
      if (pos >= text.length() || text.charAt(pos) != ':') {
        throw new JsonException("expected a colon");
      }
      pos++;
      skipSpace();
      out.put(key, value(depth));
      skipSpace();
      if (pos >= text.length()) {
        throw new JsonException("unexpected end of text");
      }
      char c = text.charAt(pos++);
      if (c == '}') {
        return out;
      }
      if (c != ',') {
        throw new JsonException("expected a comma or a closing brace");
      }
    }
  }

  private List<Object> array(int depth) {
    if (depth > MAX_DEPTH) {
      throw new JsonException("nesting is too deep");
    }
    pos++;
    List<Object> out = new ArrayList<>();
    skipSpace();
    if (pos < text.length() && text.charAt(pos) == ']') {
      pos++;
      return out;
    }
    while (true) {
      skipSpace();
      out.add(value(depth));
      skipSpace();
      if (pos >= text.length()) {
        throw new JsonException("unexpected end of text");
      }
      char c = text.charAt(pos++);
      if (c == ']') {
        return out;
      }
      if (c != ',') {
        throw new JsonException("expected a comma or a closing bracket");
      }
    }
  }

  private String string() {
    pos++;
    StringBuilder out = null;
    int start = pos;
    while (true) {
      if (pos >= text.length()) {
        throw new JsonException("unterminated string");
      }
      char c = text.charAt(pos);
      if (c == '"') {
        String tail = text.substring(start, pos);
        pos++;
        return out == null ? tail : out.append(tail).toString();
      }
      if (c < 0x20) {
        throw new JsonException("control character in string");
      }
      if (c != '\\') {
        pos++;
        continue;
      }
      if (out == null) {
        out = new StringBuilder();
      }
      out.append(text, start, pos);
      pos++;
      if (pos >= text.length()) {
        throw new JsonException("unterminated string");
      }
      char e = text.charAt(pos++);
      switch (e) {
        case '"':
          out.append('"');
          break;
        case '\\':
          out.append('\\');
          break;
        case '/':
          out.append('/');
          break;
        case 'b':
          out.append('\b');
          break;
        case 'f':
          out.append('\f');
          break;
        case 'n':
          out.append('\n');
          break;
        case 'r':
          out.append('\r');
          break;
        case 't':
          out.append('\t');
          break;
        case 'u':
          out.append(unicodeEscape());
          break;
        default:
          throw new JsonException("bad escape");
      }
      start = pos;
    }
  }

  private char unicodeEscape() {
    if (pos + 4 > text.length()) {
      throw new JsonException("bad escape");
    }
    int value = 0;
    for (int i = 0; i < 4; i++) {
      int digit = Character.digit(text.charAt(pos + i), 16);
      char c = text.charAt(pos + i);
      boolean ascii = (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
      if (digit < 0 || !ascii) {
        throw new JsonException("bad escape");
      }
      value = value * 16 + digit;
    }
    pos += 4;
    return (char) value;
  }

  private Double number() {
    int start = pos;
    if (text.charAt(pos) == '-') {
      pos++;
    }
    if (pos >= text.length()) {
      throw new JsonException("bad number");
    }
    char first = text.charAt(pos);
    if (first == '0') {
      pos++;
    } else if (first >= '1' && first <= '9') {
      digits();
    } else {
      throw new JsonException("bad number");
    }
    if (pos < text.length() && text.charAt(pos) == '.') {
      pos++;
      if (digits() == 0) {
        throw new JsonException("bad number");
      }
    }
    if (pos < text.length() && (text.charAt(pos) == 'e' || text.charAt(pos) == 'E')) {
      pos++;
      if (pos < text.length() && (text.charAt(pos) == '+' || text.charAt(pos) == '-')) {
        pos++;
      }
      if (digits() == 0) {
        throw new JsonException("bad number");
      }
    }
    return Double.valueOf(Double.parseDouble(text.substring(start, pos)));
  }

  private int digits() {
    int start = pos;
    while (pos < text.length() && text.charAt(pos) >= '0' && text.charAt(pos) <= '9') {
      pos++;
    }
    return pos - start;
  }

  /**
   * Writes a value as JSON text, as {@code JSON.stringify} would: a non-finite number is {@code
   * null}, {@code -0} is {@code 0}, and a lone surrogate is written as an escape.
   *
   * @param value a {@code null}, {@link Boolean}, {@link Number}, {@link String}, {@link List} or
   *     {@link Map} with string keys
   * @return the JSON text
   * @throws IllegalArgumentException for any other type
   */
  public static String stringify(Object value) {
    StringBuilder out = new StringBuilder();
    write(out, value);
    return out.toString();
  }

  private static void write(StringBuilder out, Object value) {
    if (value == null) {
      out.append("null");
    } else if (value instanceof Boolean || value instanceof String) {
      if (value instanceof String) {
        quote(out, (String) value);
      } else {
        out.append(value.toString());
      }
    } else if (value instanceof Number) {
      number(out, (Number) value);
    } else if (value instanceof Map) {
      out.append('{');
      boolean first = true;
      for (Map.Entry<?, ?> entry : ((Map<?, ?>) value).entrySet()) {
        if (!first) {
          out.append(',');
        }
        first = false;
        quote(out, String.valueOf(entry.getKey()));
        out.append(':');
        write(out, entry.getValue());
      }
      out.append('}');
    } else if (value instanceof List) {
      out.append('[');
      boolean first = true;
      for (Object item : (List<?>) value) {
        if (!first) {
          out.append(',');
        }
        first = false;
        write(out, item);
      }
      out.append(']');
    } else {
      throw new IllegalArgumentException("not a JSON value: " + value.getClass().getName());
    }
  }

  private static void number(StringBuilder out, Number number) {
    if (number instanceof Long || number instanceof Integer) {
      out.append(number.longValue());
      return;
    }
    double d = number.doubleValue();
    if (Double.isNaN(d) || Double.isInfinite(d)) {
      out.append("null");
    } else if (d == 0) {
      out.append('0');
    } else if (d == Math.rint(d) && Math.abs(d) < 1e15) {
      out.append((long) d);
    } else {
      out.append(Double.toString(d));
    }
  }

  private static void quote(StringBuilder out, String s) {
    out.append('"');
    int length = s.length();
    for (int i = 0; i < length; i++) {
      char c = s.charAt(i);
      switch (c) {
        case '"':
          out.append("\\\"");
          break;
        case '\\':
          out.append("\\\\");
          break;
        case '\b':
          out.append("\\b");
          break;
        case '\f':
          out.append("\\f");
          break;
        case '\n':
          out.append("\\n");
          break;
        case '\r':
          out.append("\\r");
          break;
        case '\t':
          out.append("\\t");
          break;
        default:
          if (c < 0x20) {
            out.append(String.format("\\u%04x", (int) c));
          } else if (Character.isHighSurrogate(c)
              && i + 1 < length
              && Character.isLowSurrogate(s.charAt(i + 1))) {
            out.append(c).append(s.charAt(++i));
          } else if (Character.isSurrogate(c)) {
            out.append(String.format("\\u%04x", (int) c));
          } else {
            out.append(c);
          }
      }
    }
    out.append('"');
  }
}
