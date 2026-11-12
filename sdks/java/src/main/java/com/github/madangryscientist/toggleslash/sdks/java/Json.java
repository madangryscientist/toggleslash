package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A small JSON reader and writer to plain Java values: {@code null}, {@link Boolean}, {@link
 * Double}, {@link String}, {@link List} and {@link Map}.
 *
 * <p>Numbers are IEEE-754 binary64, so {@code 1e999} reads as infinity and {@code 9007199254740993}
 * as {@code 9007199254740992}. A lone surrogate written as {@code \ud800} stays one {@code char} of
 * the resulting string. A context passed to the client may be built from this reader's output,
 * which is the way to hand the client a number or a string that Java's own types cannot spell in
 * source.
 */
public final class Json {
  /** The deepest nesting {@link #parse} accepts; the contract asks for at least 512. */
  static final int MAX_DEPTH = 1000;

  private final String text;
  private int pos;

  private Json(String text) {
    this.text = text;
  }

  /**
   * Reads one JSON value.
   *
   * @param text the document
   * @return the value
   * @throws IllegalArgumentException if {@code text} is not one JSON document or nests too deeply
   */
  public static Object parse(String text) {
    Json reader = new Json(text);
    reader.skipWhitespace();
    Object value = reader.value(0);
    reader.skipWhitespace();
    if (reader.pos != text.length()) {
      throw reader.fail("unexpected data after the value");
    }
    return value;
  }

  private IllegalArgumentException fail(String why) {
    return new IllegalArgumentException("bad JSON at " + pos + ": " + why);
  }

  private void skipWhitespace() {
    while (pos < text.length()) {
      char c = text.charAt(pos);
      if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
        pos++;
      } else {
        break;
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
        return number();
    }
  }

  private void literal(String word) {
    if (!text.startsWith(word, pos)) {
      throw fail("expected " + word);
    }
    pos += word.length();
  }

  private Map<String, Object> object(int depth) {
    if (depth > MAX_DEPTH) {
      throw fail("nested too deeply");
    }
    pos++;
    Map<String, Object> result = new LinkedHashMap<>();
    skipWhitespace();
    if (pos < text.length() && text.charAt(pos) == '}') {
      pos++;
      return result;
    }
    while (true) {
      skipWhitespace();
      if (pos >= text.length() || text.charAt(pos) != '"') {
        throw fail("expected a string key");
      }
      String key = string();
      skipWhitespace();
      if (pos >= text.length() || text.charAt(pos) != ':') {
        throw fail("expected ':'");
      }
      pos++;
      skipWhitespace();
      result.put(key, value(depth));
      skipWhitespace();
      if (pos >= text.length()) {
        throw fail("unexpected end");
      }
      char c = text.charAt(pos++);
      if (c == '}') {
        return result;
      }
      if (c != ',') {
        throw fail("expected ',' or '}'");
      }
    }
  }

  private List<Object> array(int depth) {
    if (depth > MAX_DEPTH) {
      throw fail("nested too deeply");
    }
    pos++;
    List<Object> result = new ArrayList<>();
    skipWhitespace();
    if (pos < text.length() && text.charAt(pos) == ']') {
      pos++;
      return result;
    }
    while (true) {
      skipWhitespace();
      result.add(value(depth));
      skipWhitespace();
      if (pos >= text.length()) {
        throw fail("unexpected end");
      }
      char c = text.charAt(pos++);
      if (c == ']') {
        return result;
      }
      if (c != ',') {
        throw fail("expected ',' or ']'");
      }
    }
  }

  private String string() {
    pos++;
    StringBuilder out = null;
    int start = pos;
    while (true) {
      if (pos >= text.length()) {
        throw fail("unterminated string");
      }
      char c = text.charAt(pos);
      if (c == '"') {
        String result =
            out == null ? text.substring(start, pos) : out.append(text, start, pos).toString();
        pos++;
        return result;
      }
      if (c < 0x20) {
        throw fail("control character in string");
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
        throw fail("unterminated escape");
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
          if (pos + 4 > text.length()) {
            throw fail("short \\u escape");
          }
          int unit = 0;
          for (int i = 0; i < 4; i++) {
            int digit = Character.digit(text.charAt(pos + i), 16);
            if (digit < 0 || text.charAt(pos + i) > 'f') {
              throw fail("bad \\u escape");
            }
            unit = unit * 16 + digit;
          }
          pos += 4;
          out.append((char) unit);
          break;
        default:
          throw fail("bad escape");
      }
      start = pos;
    }
  }

  private Double number() {
    int start = pos;
    if (pos < text.length() && text.charAt(pos) == '-') {
      pos++;
    }
    if (pos >= text.length()) {
      throw fail("bad number");
    }
    if (text.charAt(pos) == '0') {
      pos++;
    } else if (isDigit(text.charAt(pos))) {
      digits();
    } else {
      throw fail("unexpected character");
    }
    if (pos < text.length() && text.charAt(pos) == '.') {
      pos++;
      if (pos >= text.length() || !isDigit(text.charAt(pos))) {
        throw fail("bad fraction");
      }
      digits();
    }
    if (pos < text.length() && (text.charAt(pos) == 'e' || text.charAt(pos) == 'E')) {
      pos++;
      if (pos < text.length() && (text.charAt(pos) == '+' || text.charAt(pos) == '-')) {
        pos++;
      }
      if (pos >= text.length() || !isDigit(text.charAt(pos))) {
        throw fail("bad exponent");
      }
      digits();
    }
    return Double.parseDouble(text.substring(start, pos));
  }

  private void digits() {
    while (pos < text.length() && isDigit(text.charAt(pos))) {
      pos++;
    }
  }

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  /**
   * Writes a value as JSON. Lone surrogates are written as {@code \}{@code u} escapes, so the
   * output is valid UTF-8 text whatever the input; a number that is not finite is written as {@code
   * null}.
   *
   * @param value {@code null}, a {@link Boolean}, a {@link Number}, a {@link CharSequence}, a
   *     {@link List}, an array of objects or a {@link Map} with string keys; anything else is
   *     written as its {@code toString()}
   * @return the JSON text
   */
  public static String write(Object value) {
    StringBuilder out = new StringBuilder();
    write(out, value);
    return out.toString();
  }

  private static void write(StringBuilder out, Object value) {
    if (value == null) {
      out.append("null");
    } else if (value instanceof Boolean) {
      out.append(value.toString());
    } else if (value instanceof Double || value instanceof Float) {
      double d = ((Number) value).doubleValue();
      if (Double.isNaN(d) || Double.isInfinite(d)) {
        out.append("null");
      } else if (d == Math.rint(d) && Math.abs(d) < 1e15) {
        out.append(d == 0 && 1 / d < 0 ? "-0" : Long.toString((long) d));
      } else {
        out.append(Double.toString(d));
      }
    } else if (value instanceof Number) {
      out.append(value.toString());
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
    } else if (value instanceof Iterable) {
      out.append('[');
      boolean first = true;
      for (Object item : (Iterable<?>) value) {
        if (!first) {
          out.append(',');
        }
        first = false;
        write(out, item);
      }
      out.append(']');
    } else if (value instanceof Object[]) {
      out.append('[');
      Object[] items = (Object[]) value;
      for (int i = 0; i < items.length; i++) {
        if (i > 0) {
          out.append(',');
        }
        write(out, items[i]);
      }
      out.append(']');
    } else {
      quote(out, value.toString());
    }
  }

  private static void quote(StringBuilder out, String text) {
    out.append('"');
    int length = text.length();
    for (int i = 0; i < length; i++) {
      char c = text.charAt(i);
      switch (c) {
        case '"':
          out.append("\\\"");
          break;
        case '\\':
          out.append("\\\\");
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
          boolean lone = false;
          if (Character.isHighSurrogate(c)) {
            if (i + 1 < length && Character.isLowSurrogate(text.charAt(i + 1))) {
              out.append(c).append(text.charAt(++i));
              continue;
            }
            lone = true;
          } else if (Character.isLowSurrogate(c)) {
            lone = true;
          }
          if (c < 0x20 || lone) {
            out.append(String.format("\\u%04x", (int) c));
          } else {
            out.append(c);
          }
      }
    }
    out.append('"');
  }
}
