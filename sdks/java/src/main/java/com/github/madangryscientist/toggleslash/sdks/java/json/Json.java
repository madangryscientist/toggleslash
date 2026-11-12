package com.github.madangryscientist.toggleslash.sdks.java.json;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A strict JSON parser and a writer. Values are {@code null}, {@link Boolean}, {@link Double},
 * {@link String}, {@link List} and {@link Map} (insertion ordered). Numbers are binary64, so {@code
 * 1e999} is infinity. A lone surrogate in a string is kept as the one code unit. A duplicate key
 * keeps the last value. Nesting past {@value #MAX_DEPTH} levels is an error.
 */
public final class Json {
  /** The deepest nesting of arrays and objects accepted. */
  public static final int MAX_DEPTH = 1024;

  private final String text;
  private int pos;

  private Json(String text) {
    this.text = text;
  }

  /** Parses {@code text}, which must be exactly one JSON value. */
  public static Object parse(String text) {
    Json parser = new Json(text);
    parser.skipSpace();
    Object value = parser.value(0);
    parser.skipSpace();
    if (parser.pos != text.length()) {
      throw parser.error("unexpected text after the value");
    }
    return value;
  }

  private JsonException error(String what) {
    return new JsonException(what + " at " + pos);
  }

  private void skipSpace() {
    while (pos < text.length()) {
      char c = text.charAt(pos);
      if (c != ' ' && c != '\t' && c != '\n' && c != '\r') {
        return;
      }
      pos++;
    }
  }

  private Object value(int depth) {
    if (pos >= text.length()) {
      throw error("unexpected end");
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
      throw error("bad literal");
    }
    pos += word.length();
  }

  private Map<String, Object> object(int depth) {
    if (depth > MAX_DEPTH) {
      throw error("nested too deeply");
    }
    pos++;
    Map<String, Object> map = new LinkedHashMap<>();
    skipSpace();
    if (peek() == '}') {
      pos++;
      return map;
    }
    while (true) {
      skipSpace();
      if (peek() != '"') {
        throw error("expected a key");
      }
      String key = string();
      skipSpace();
      if (peek() != ':') {
        throw error("expected a colon");
      }
      pos++;
      skipSpace();
      map.put(key, value(depth));
      skipSpace();
      char c = peek();
      pos++;
      if (c == '}') {
        return map;
      }
      if (c != ',') {
        throw error("expected a comma or a closing brace");
      }
    }
  }

  private List<Object> array(int depth) {
    if (depth > MAX_DEPTH) {
      throw error("nested too deeply");
    }
    pos++;
    List<Object> list = new ArrayList<>();
    skipSpace();
    if (peek() == ']') {
      pos++;
      return list;
    }
    while (true) {
      skipSpace();
      list.add(value(depth));
      skipSpace();
      char c = peek();
      pos++;
      if (c == ']') {
        return list;
      }
      if (c != ',') {
        throw error("expected a comma or a closing bracket");
      }
    }
  }

  /** The current character, or NUL at the end (NUL is never valid where this is used). */
  private char peek() {
    return pos < text.length() ? text.charAt(pos) : '\0';
  }

  private String string() {
    pos++;
    StringBuilder out = null;
    int start = pos;
    while (true) {
      if (pos >= text.length()) {
        throw error("unterminated string");
      }
      char c = text.charAt(pos);
      if (c == '"') {
        String piece = text.substring(start, pos);
        pos++;
        return out == null ? piece : out.append(piece).toString();
      }
      if (c < 0x20) {
        throw error("control character in string");
      }
      if (c == '\\') {
        if (out == null) {
          out = new StringBuilder();
        }
        out.append(text, start, pos);
        pos++;
        out.append(escape());
        start = pos;
      } else {
        pos++;
      }
    }
  }

  private char escape() {
    if (pos >= text.length()) {
      throw error("unterminated escape");
    }
    char c = text.charAt(pos++);
    switch (c) {
      case '"':
        return '"';
      case '\\':
        return '\\';
      case '/':
        return '/';
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
        throw error("bad escape");
    }
  }

  private char unicode() {
    if (pos + 4 > text.length()) {
      throw error("bad unicode escape");
    }
    int value = 0;
    for (int i = 0; i < 4; i++) {
      int digit = Character.digit(text.charAt(pos + i), 16);
      char c = text.charAt(pos + i);
      if (digit < 0 || c > 'f') {
        throw error("bad unicode escape");
      }
      value = (value << 4) | digit;
    }
    pos += 4;
    return (char) value;
  }

  private Double number() {
    int start = pos;
    if (peek() == '-') {
      pos++;
    }
    if (peek() == '0') {
      pos++;
    } else if (isDigit(peek())) {
      digits();
    } else {
      throw error("bad value");
    }
    if (peek() == '.') {
      pos++;
      if (!isDigit(peek())) {
        throw error("bad number");
      }
      digits();
    }
    if (peek() == 'e' || peek() == 'E') {
      pos++;
      if (peek() == '+' || peek() == '-') {
        pos++;
      }
      if (!isDigit(peek())) {
        throw error("bad number");
      }
      digits();
    }
    return Double.valueOf(Double.parseDouble(text.substring(start, pos)));
  }

  private void digits() {
    while (isDigit(peek())) {
      pos++;
    }
  }

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  // ---- writing ----

  /** Writes a value in the model above as JSON; a non-finite number is an error. */
  public static String write(Object value) {
    StringBuilder out = new StringBuilder();
    write(value, out);
    return out.toString();
  }

  /** Appends a value as JSON. */
  public static void write(Object value, StringBuilder out) {
    if (value == null) {
      out.append("null");
    } else if (value instanceof Boolean) {
      out.append(((Boolean) value).booleanValue() ? "true" : "false");
    } else if (value instanceof Number) {
      number((Number) value, out);
    } else if (value instanceof CharSequence) {
      quote(value.toString(), out);
    } else if (value instanceof List<?>) {
      out.append('[');
      boolean first = true;
      for (Object item : (List<?>) value) {
        if (!first) {
          out.append(',');
        }
        first = false;
        write(item, out);
      }
      out.append(']');
    } else if (value instanceof Map<?, ?>) {
      out.append('{');
      boolean first = true;
      for (Map.Entry<?, ?> entry : ((Map<?, ?>) value).entrySet()) {
        if (!first) {
          out.append(',');
        }
        first = false;
        quote(String.valueOf(entry.getKey()), out);
        out.append(':');
        write(entry.getValue(), out);
      }
      out.append('}');
    } else {
      throw new JsonException("cannot write " + value.getClass().getName());
    }
  }

  private static void number(Number number, StringBuilder out) {
    double d = number.doubleValue();
    if (Double.isNaN(d) || Double.isInfinite(d)) {
      throw new JsonException("a non-finite number has no JSON form");
    }
    if (number instanceof Long || number instanceof Integer) {
      out.append(number.longValue());
    } else if (d == Math.rint(d) && Math.abs(d) < 1e15) {
      out.append((long) d);
    } else {
      out.append(Double.toString(d));
    }
  }

  private static void quote(String s, StringBuilder out) {
    out.append('"');
    for (int i = 0; i < s.length(); i++) {
      char c = s.charAt(i);
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
          if (c < 0x20 || (c >= 0xD800 && c <= 0xDFFF && !pairedSurrogate(s, i))) {
            out.append(String.format("\\u%04x", (int) c));
          } else {
            out.append(c);
          }
      }
    }
    out.append('"');
  }

  /** True when the surrogate at {@code i} is half of a valid pair (so it is written as it is). */
  private static boolean pairedSurrogate(String s, int i) {
    char c = s.charAt(i);
    if (Character.isHighSurrogate(c)) {
      return i + 1 < s.length() && Character.isLowSurrogate(s.charAt(i + 1));
    }
    return i > 0 && Character.isHighSurrogate(s.charAt(i - 1));
  }
}
