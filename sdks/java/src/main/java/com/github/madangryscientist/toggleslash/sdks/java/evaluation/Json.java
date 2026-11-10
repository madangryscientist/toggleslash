package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * The JSON reader and writer. The JDK has no JSON parser, and the spec pins what a parser must
 * keep: numbers are binary64 ({@code 1e999} is Infinity, not an error), negative zero is kept, and
 * a lone surrogate in a string stays as it is.
 *
 * <p>A value is a tree of {@code LinkedHashMap<String,Object>}, {@code ArrayList<Object>}, {@code
 * String}, {@code Double}, {@code Boolean} and {@code null}.
 */
public final class Json {
  /** Nesting deeper than this is an error, so no input can overflow the stack. */
  static final int MAX_DEPTH = 512;

  private final String text;
  private int pos;

  private Json(String text) {
    this.text = text;
  }

  /** Reads the bytes of a response body: UTF-8 with U+FFFD for bad bytes, a leading BOM skipped. */
  public static Object parse(byte[] body) {
    String decoded = new String(body, StandardCharsets.UTF_8);
    return parse(decoded.startsWith("﻿") ? decoded.substring(1) : decoded);
  }

  /** Reads JSON text. A duplicate key keeps the last value. Throws {@link JsonException}. */
  public static Object parse(String text) {
    Json reader = new Json(text);
    reader.skipSpace();
    Object value = reader.value(0);
    reader.skipSpace();
    if (reader.pos != text.length()) {
      throw new JsonException("unexpected content after the value");
    }
    return value;
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

  private char peek() {
    if (pos >= text.length()) {
      throw new JsonException("unexpected end of input");
    }
    return text.charAt(pos);
  }

  private Object value(int depth) {
    if (depth > MAX_DEPTH) {
      throw new JsonException("nested too deeply");
    }
    char c = peek();
    switch (c) {
      case '{':
        return object(depth);
      case '[':
        return array(depth);
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
      throw new JsonException("invalid literal");
    }
    pos += word.length();
  }

  private Map<String, Object> object(int depth) {
    Map<String, Object> map = new LinkedHashMap<>();
    pos++;
    skipSpace();
    if (peek() == '}') {
      pos++;
      return map;
    }
    while (true) {
      skipSpace();
      if (peek() != '"') {
        throw new JsonException("expected a member name");
      }
      String name = string();
      skipSpace();
      if (peek() != ':') {
        throw new JsonException("expected ':'");
      }
      pos++;
      skipSpace();
      map.put(name, value(depth + 1));
      skipSpace();
      char c = peek();
      pos++;
      if (c == '}') {
        return map;
      }
      if (c != ',') {
        throw new JsonException("expected ',' or '}'");
      }
    }
  }

  private List<Object> array(int depth) {
    List<Object> list = new ArrayList<>();
    pos++;
    skipSpace();
    if (peek() == ']') {
      pos++;
      return list;
    }
    while (true) {
      skipSpace();
      list.add(value(depth + 1));
      skipSpace();
      char c = peek();
      pos++;
      if (c == ']') {
        return list;
      }
      if (c != ',') {
        throw new JsonException("expected ',' or ']'");
      }
    }
  }

  private String string() {
    pos++;
    StringBuilder out = null;
    int start = pos;
    while (true) {
      char c = peek();
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
      out.append(escape());
      start = pos;
    }
  }

  private char escape() {
    char c = peek();
    pos++;
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
        throw new JsonException("invalid escape");
    }
  }

  private char unicode() {
    if (pos + 4 > text.length()) {
      throw new JsonException("short unicode escape");
    }
    int code = 0;
    for (int i = 0; i < 4; i++) {
      int digit = Character.digit(text.charAt(pos + i), 16);
      char d = text.charAt(pos + i);
      boolean ascii = d < 0x80;
      if (digit < 0 || !ascii) {
        throw new JsonException("invalid unicode escape");
      }
      code = code * 16 + digit;
    }
    pos += 4;
    return (char) code;
  }

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  private Double number() {
    int start = pos;
    if (peek() == '-') {
      pos++;
    }
    char first = peek();
    if (first == '0') {
      pos++;
    } else if (first >= '1' && first <= '9') {
      digits();
    } else {
      throw new JsonException("invalid value");
    }
    if (pos < text.length() && text.charAt(pos) == '.') {
      pos++;
      digits();
    }
    if (pos < text.length() && (text.charAt(pos) == 'e' || text.charAt(pos) == 'E')) {
      pos++;
      if (pos < text.length() && (text.charAt(pos) == '+' || text.charAt(pos) == '-')) {
        pos++;
      }
      digits();
    }
    return Double.valueOf(Double.parseDouble(text.substring(start, pos)));
  }

  private void digits() {
    if (pos >= text.length() || !isDigit(text.charAt(pos))) {
      throw new JsonException("expected a digit");
    }
    while (pos < text.length() && isDigit(text.charAt(pos))) {
      pos++;
    }
  }

  /**
   * Writes a value as JSON. Infinity is {@code 1e999}, negative zero is {@code -0}, and a lone
   * surrogate is {@code \}{@code udXXX}. NaN cannot be written in JSON and becomes {@code null}.
   */
  public static String write(Object value) {
    StringBuilder out = new StringBuilder();
    write(out, value);
    return out.toString();
  }

  private static void write(StringBuilder out, Object value) {
    if (value == null) {
      out.append("null");
    } else if (value instanceof String s) {
      writeString(out, s);
    } else if (value instanceof Boolean b) {
      out.append(b.booleanValue() ? "true" : "false");
    } else if (value instanceof Double d) {
      writeNumber(out, d.doubleValue());
    } else if (value instanceof Number n) {
      if (n instanceof Long || n instanceof Integer) {
        out.append(n.longValue());
      } else {
        writeNumber(out, n.doubleValue());
      }
    } else if (value instanceof Map<?, ?> map) {
      out.append('{');
      boolean first = true;
      for (Map.Entry<?, ?> entry : map.entrySet()) {
        if (!first) {
          out.append(',');
        }
        first = false;
        writeString(out, String.valueOf(entry.getKey()));
        out.append(':');
        write(out, entry.getValue());
      }
      out.append('}');
    } else if (value instanceof List<?> list) {
      out.append('[');
      for (int i = 0; i < list.size(); i++) {
        if (i > 0) {
          out.append(',');
        }
        write(out, list.get(i));
      }
      out.append(']');
    } else {
      writeString(out, value.toString());
    }
  }

  private static void writeNumber(StringBuilder out, double d) {
    if (Double.isNaN(d)) {
      out.append("null");
    } else if (Double.isInfinite(d)) {
      out.append(d > 0 ? "1e999" : "-1e999");
    } else if (d == 0) {
      out.append(1 / d < 0 ? "-0" : "0");
    } else if (d == Math.rint(d) && Math.abs(d) < 1e15) {
      out.append((long) d);
    } else {
      out.append(Double.toString(d));
    }
  }

  private static void writeString(StringBuilder out, String s) {
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
      } else if (c < 0x20 || isLoneSurrogate(s, i)) {
        out.append(String.format("\\u%04x", (int) c));
      } else {
        out.append(c);
      }
    }
    out.append('"');
  }

  private static boolean isLoneSurrogate(String s, int i) {
    char c = s.charAt(i);
    if (Character.isHighSurrogate(c)) {
      return i + 1 >= s.length() || !Character.isLowSurrogate(s.charAt(i + 1));
    }
    return Character.isLowSurrogate(c) && (i == 0 || !Character.isHighSurrogate(s.charAt(i - 1)));
  }
}
