package com.github.madangryscientist.toggleslash.sdks.java.json;

import java.math.BigDecimal;
import java.math.BigInteger;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A strict JSON parser and a small writer.
 *
 * <p>A parsed value is {@code null}, a {@link Boolean}, a {@link Double}, a {@link String}, an
 * unmodifiable {@link List} of values or an unmodifiable {@link Map} (in document order) from
 * {@link String} to value. Numbers are IEEE-754 binary64: {@code 1e999} is infinity and {@code
 * 9007199254740993} reads as {@code 9007199254740992}. A duplicate key keeps the last value. A lone
 * surrogate written as an escape is kept as it is in the {@link String}.
 */
public final class Json {
  /** The deepest nesting the parser accepts. The contract needs at least 512 levels. */
  public static final int MAX_DEPTH = 1024;

  private final String text;
  private int pos;

  private Json(String text) {
    this.text = text;
  }

  /**
   * Parses one JSON document.
   *
   * @param text the document, with no byte order mark
   * @return the value
   * @throws JsonException when the text is not one JSON value, or is nested deeper than {@link
   *     #MAX_DEPTH}
   */
  public static Object parse(String text) {
    Json parser = new Json(text);
    parser.skipSpace();
    Object value = parser.value(0);
    parser.skipSpace();
    if (parser.pos != text.length()) {
      throw new JsonException("unexpected content after the document");
    }
    return value;
  }

  private JsonException error(String what) {
    return new JsonException(what + " at offset " + pos);
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
        if (c == '-' || (c >= '0' && c <= '9')) {
          return number();
        }
        throw error("unexpected character");
    }
  }

  private void literal(String word) {
    if (!text.startsWith(word, pos)) {
      throw error("bad literal");
    }
    pos += word.length();
  }

  private Object object(int depth) {
    if (depth > MAX_DEPTH) {
      throw error("nested too deeply");
    }
    pos++;
    Map<String, Object> map = new LinkedHashMap<>();
    skipSpace();
    if (peek() == '}') {
      pos++;
      return Collections.unmodifiableMap(map);
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
        return Collections.unmodifiableMap(map);
      }
      if (c != ',') {
        throw error("expected a comma or a closing brace");
      }
    }
  }

  private Object array(int depth) {
    if (depth > MAX_DEPTH) {
      throw error("nested too deeply");
    }
    pos++;
    List<Object> list = new ArrayList<>();
    skipSpace();
    if (peek() == ']') {
      pos++;
      return Collections.unmodifiableList(list);
    }
    while (true) {
      skipSpace();
      list.add(value(depth));
      skipSpace();
      char c = peek();
      pos++;
      if (c == ']') {
        return Collections.unmodifiableList(list);
      }
      if (c != ',') {
        throw error("expected a comma or a closing bracket");
      }
    }
  }

  private char peek() {
    if (pos >= text.length()) {
      throw error("unexpected end");
    }
    return text.charAt(pos);
  }

  private String string() {
    pos++;
    int start = pos;
    // The common case has no escape: slice it out whole.
    while (pos < text.length()) {
      char c = text.charAt(pos);
      if (c == '"') {
        String slice = text.substring(start, pos);
        pos++;
        return slice;
      }
      if (c == '\\') {
        break;
      }
      if (c < 0x20) {
        throw error("control character in a string");
      }
      pos++;
    }
    StringBuilder out = new StringBuilder(text.substring(start, pos));
    while (pos < text.length()) {
      char c = text.charAt(pos++);
      if (c == '"') {
        return out.toString();
      }
      if (c < 0x20) {
        throw error("control character in a string");
      }
      if (c != '\\') {
        out.append(c);
        continue;
      }
      if (pos >= text.length()) {
        break;
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
          out.append(hex4());
          break;
        default:
          throw error("bad escape");
      }
    }
    throw error("unterminated string");
  }

  private char hex4() {
    if (pos + 4 > text.length()) {
      throw error("bad unicode escape");
    }
    int value = 0;
    for (int i = 0; i < 4; i++) {
      int digit = Character.digit(text.charAt(pos + i), 16);
      char c = text.charAt(pos + i);
      boolean ascii = (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
      if (digit < 0 || !ascii) {
        throw error("bad unicode escape");
      }
      value = value * 16 + digit;
    }
    pos += 4;
    return (char) value;
  }

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  private Object number() {
    int start = pos;
    if (text.charAt(pos) == '-') {
      pos++;
    }
    if (pos >= text.length() || !isDigit(text.charAt(pos))) {
      throw error("bad number");
    }
    if (text.charAt(pos) == '0') {
      pos++;
    } else {
      while (pos < text.length() && isDigit(text.charAt(pos))) {
        pos++;
      }
    }
    if (pos < text.length() && text.charAt(pos) == '.') {
      pos++;
      if (pos >= text.length() || !isDigit(text.charAt(pos))) {
        throw error("bad number");
      }
      while (pos < text.length() && isDigit(text.charAt(pos))) {
        pos++;
      }
    }
    if (pos < text.length() && (text.charAt(pos) == 'e' || text.charAt(pos) == 'E')) {
      pos++;
      if (pos < text.length() && (text.charAt(pos) == '+' || text.charAt(pos) == '-')) {
        pos++;
      }
      if (pos >= text.length() || !isDigit(text.charAt(pos))) {
        throw error("bad number");
      }
      while (pos < text.length() && isDigit(text.charAt(pos))) {
        pos++;
      }
    }
    return Double.valueOf(Double.parseDouble(text.substring(start, pos)));
  }

  // ---- writing ----

  /**
   * Writes a value as JSON text that is pure ASCII: every character outside printable ASCII is a
   * {@code \}{@code uXXXX} escape, so a lone surrogate survives a trip through any byte encoding.
   *
   * <p>Accepted: {@code null}, {@link Boolean}, any {@link Number} (a non-finite one is written as
   * {@code null}), {@link CharSequence}, {@link Map} with string keys, {@link List} and {@code
   * Object[]}. Anything else is written as {@code null}.
   *
   * @param value the value
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
    } else if (value instanceof Number) {
      writeNumber(out, (Number) value);
    } else if (value instanceof CharSequence) {
      writeString(out, value.toString());
    } else if (value instanceof Map) {
      writeMap(out, (Map<?, ?>) value);
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
    } else if (value instanceof Object[]) {
      write(out, List.of((Object[]) value));
    } else {
      out.append("null");
    }
  }

  private static void writeMap(StringBuilder out, Map<?, ?> map) {
    out.append('{');
    boolean first = true;
    for (Map.Entry<?, ?> entry : map.entrySet()) {
      if (!(entry.getKey() instanceof String)) {
        continue;
      }
      if (!first) {
        out.append(',');
      }
      first = false;
      writeString(out, (String) entry.getKey());
      out.append(':');
      write(out, entry.getValue());
    }
    out.append('}');
  }

  private static void writeNumber(StringBuilder out, Number number) {
    if (number instanceof BigInteger || number instanceof BigDecimal) {
      out.append(number instanceof BigDecimal ? ((BigDecimal) number).toPlainString() : number);
      return;
    }
    if (number instanceof Double || number instanceof Float) {
      double d = number.doubleValue();
      if (Double.isNaN(d) || Double.isInfinite(d)) {
        out.append("null");
      } else if (d == Math.rint(d) && Math.abs(d) < 1e15) {
        if (d == 0 && 1 / d < 0) {
          out.append("-0");
        } else {
          out.append((long) d);
        }
      } else {
        out.append(Double.toString(d));
      }
      return;
    }
    out.append(number.toString());
  }

  /**
   * Appends a JSON string literal, ASCII only, to the builder.
   *
   * @param out where to append
   * @param s the string, a lone surrogate included
   */
  public static void writeString(StringBuilder out, String s) {
    out.append('"');
    for (int i = 0; i < s.length(); i++) {
      char c = s.charAt(i);
      if (c == '"') {
        out.append("\\\"");
      } else if (c == '\\') {
        out.append("\\\\");
      } else if (c >= 0x20 && c < 0x7f) {
        out.append(c);
      } else {
        out.append("\\u");
        String hex = Integer.toHexString(c);
        for (int pad = hex.length(); pad < 4; pad++) {
          out.append('0');
        }
        out.append(hex);
      }
    }
    out.append('"');
  }
}
