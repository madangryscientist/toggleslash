package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A strict JSON reader and writer over plain JDK values.
 *
 * <p>The tree is {@code null}, {@link Boolean}, {@link Double}, {@link String}, {@link List} of
 * values and {@link Map} from {@link String} to values. Every number is a binary64 {@link Double},
 * so {@code 1e999} is infinity and {@code 9007199254740993} reads as {@code 9007199254740992}. A
 * duplicate object key keeps the last value. A lone surrogate in a string is kept as it is.
 */
public final class Json {
  /** The deepest nesting {@link #parse(String)} accepts; the contract's floor is 512. */
  public static final int MAX_DEPTH = 1000;

  private final String text;
  private int pos;

  private Json(String text) {
    this.text = text;
  }

  /**
   * Parses one JSON text.
   *
   * @param text the text, without a byte order mark
   * @return the tree
   * @throws JsonException when the text is not JSON or nests deeper than {@link #MAX_DEPTH}
   */
  public static Object parse(String text) throws JsonException {
    Json reader = new Json(text);
    reader.skipSpace();
    Object value = reader.value(0);
    reader.skipSpace();
    if (reader.pos != text.length()) {
      throw reader.fail("unexpected text after the value");
    }
    return value;
  }

  private JsonException fail(String what) {
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

  private Object value(int depth) throws JsonException {
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

  private void literal(String word) throws JsonException {
    if (!text.startsWith(word, pos)) {
      throw fail("bad literal");
    }
    pos += word.length();
  }

  private Map<String, Object> object(int depth) throws JsonException {
    if (depth > MAX_DEPTH) {
      throw fail("nesting too deep");
    }
    pos++;
    Map<String, Object> out = new LinkedHashMap<>();
    skipSpace();
    if (peek() == '}') {
      pos++;
      return out;
    }
    while (true) {
      skipSpace();
      if (peek() != '"') {
        throw fail("expected a key");
      }
      String key = string();
      skipSpace();
      if (peek() != ':') {
        throw fail("expected a colon");
      }
      pos++;
      skipSpace();
      out.put(key, value(depth));
      skipSpace();
      char c = peek();
      pos++;
      if (c == '}') {
        return out;
      }
      if (c != ',') {
        throw fail("expected a comma or }");
      }
    }
  }

  private List<Object> array(int depth) throws JsonException {
    if (depth > MAX_DEPTH) {
      throw fail("nesting too deep");
    }
    pos++;
    List<Object> out = new ArrayList<>();
    skipSpace();
    if (peek() == ']') {
      pos++;
      return out;
    }
    while (true) {
      skipSpace();
      out.add(value(depth));
      skipSpace();
      char c = peek();
      pos++;
      if (c == ']') {
        return out;
      }
      if (c != ',') {
        throw fail("expected a comma or ]");
      }
    }
  }

  private char peek() {
    return pos < text.length() ? text.charAt(pos) : '\0';
  }

  private String string() throws JsonException {
    pos++;
    StringBuilder out = new StringBuilder();
    while (true) {
      if (pos >= text.length()) {
        throw fail("unterminated string");
      }
      char c = text.charAt(pos++);
      if (c == '"') {
        return out.toString();
      }
      if (c < 0x20) {
        throw fail("control character in string");
      }
      if (c != '\\') {
        out.append(c);
        continue;
      }
      if (pos >= text.length()) {
        throw fail("unterminated escape");
      }
      char e = text.charAt(pos++);
      switch (e) {
        case '"':
        case '\\':
        case '/':
          out.append(e);
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
          out.append(unit());
          break;
        default:
          throw fail("bad escape");
      }
    }
  }

  private char unit() throws JsonException {
    if (pos + 4 > text.length()) {
      throw fail("short \\u escape");
    }
    int value = 0;
    for (int i = 0; i < 4; i++) {
      int digit = Character.digit(text.charAt(pos + i), 16);
      char c = text.charAt(pos + i);
      if (digit < 0 || c > 'f') {
        throw fail("bad \\u escape");
      }
      value = value * 16 + digit;
    }
    pos += 4;
    return (char) value;
  }

  private static boolean digit(char c) {
    return c >= '0' && c <= '9';
  }

  private Double number() throws JsonException {
    int start = pos;
    if (peek() == '-') {
      pos++;
    }
    if (peek() == '0') {
      pos++;
    } else if (digit(peek())) {
      while (digit(peek())) {
        pos++;
      }
    } else {
      throw fail("unexpected character");
    }
    if (peek() == '.') {
      pos++;
      if (!digit(peek())) {
        throw fail("bad fraction");
      }
      while (digit(peek())) {
        pos++;
      }
    }
    if (peek() == 'e' || peek() == 'E') {
      pos++;
      if (peek() == '+' || peek() == '-') {
        pos++;
      }
      if (!digit(peek())) {
        throw fail("bad exponent");
      }
      while (digit(peek())) {
        pos++;
      }
    }
    return Double.parseDouble(text.substring(start, pos));
  }

  /**
   * Writes a tree as JSON text made only of ASCII, so it is valid UTF-8 whatever the strings hold.
   * A non-finite number is written as {@code null}; a {@code Number} of any class is written as its
   * binary64 value; a value of any other class is written as {@code null}.
   *
   * @param value the tree
   * @return the text
   */
  public static String stringify(Object value) {
    StringBuilder out = new StringBuilder();
    write(out, value);
    return out.toString();
  }

  private static void write(StringBuilder out, Object value) {
    if (value == null) {
      out.append("null");
    } else if (value instanceof Boolean || value instanceof Long || value instanceof Integer) {
      out.append(value);
    } else if (value instanceof Number number) {
      writeNumber(out, number.doubleValue());
    } else if (value instanceof String string) {
      writeString(out, string);
    } else if (value instanceof List<?> list) {
      out.append('[');
      boolean first = true;
      for (Object item : list) {
        if (!first) {
          out.append(',');
        }
        first = false;
        write(out, item);
      }
      out.append(']');
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
    } else {
      out.append("null");
    }
  }

  private static void writeNumber(StringBuilder out, double d) {
    if (Double.isNaN(d) || Double.isInfinite(d)) {
      out.append("null");
    } else if (d == 0) {
      out.append(1 / d < 0 ? "-0" : "0");
    } else if (d == Math.rint(d) && Math.abs(d) < 1e15) {
      out.append((long) d);
    } else {
      out.append(d);
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
      } else if (c >= 0x20 && c < 0x7f) {
        out.append(c);
      } else {
        out.append(String.format("\\u%04x", (int) c));
      }
    }
    out.append('"');
  }
}
