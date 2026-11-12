package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A JSON parser that keeps what the evaluation spec needs. Numbers are binary64 ({@link Double}),
 * so {@code 1e999} is infinity and {@code 9007199254740993} reads as {@code 9007199254740992}. A
 * string is a Java {@link String}, so a lone surrogate written as {@code "\ud800"} stays one {@code
 * char}. A duplicate key keeps the last value.
 *
 * <p>Values come out as {@code null}, {@link Boolean}, {@link Double}, {@link String}, {@link List}
 * of these and {@link Map} from {@link String} to these.
 */
public final class Json {

  /** Nesting deeper than this is refused (the contract's minimum is 512). */
  static final int MAX_DEPTH = 1000;

  private static final String BOM = String.valueOf((char) 0xFEFF);

  private final String text;
  private int pos;

  private Json(String text) {
    this.text = text;
  }

  /** Thrown for text that is not JSON. */
  public static final class ParseException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    ParseException(String message) {
      super(message);
    }
  }

  /**
   * Parses UTF-8 bytes. Invalid bytes become U+FFFD, one per maximal invalid subpart, and a leading
   * byte order mark is skipped.
   */
  public static Object parse(byte[] bytes) {
    return parse(decodeUtf8(bytes));
  }

  /** Parses JSON text. */
  public static Object parse(String text) {
    Json json = new Json(text);
    if (text.startsWith(BOM)) {
      json.pos = 1;
    }
    json.skipWhitespace();
    Object value = json.value(0);
    json.skipWhitespace();
    if (json.pos != text.length()) {
      throw json.error("unexpected content after the value");
    }
    return value;
  }

  /** Decodes UTF-8 with U+FFFD for invalid input, as the WHATWG decoder does. */
  public static String decodeUtf8(byte[] bytes) {
    return Utf8Decoder.decode(bytes, 0, bytes.length);
  }

  private ParseException error(String message) {
    return new ParseException(message + " at " + pos);
  }

  private void skipWhitespace() {
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

  private Map<String, Object> object(int depth) {
    if (depth > MAX_DEPTH) {
      throw error("nested too deeply");
    }
    pos++;
    Map<String, Object> map = new LinkedHashMap<>();
    skipWhitespace();
    if (peek() == '}') {
      pos++;
      return map;
    }
    while (true) {
      skipWhitespace();
      if (peek() != '"') {
        throw error("expected a key");
      }
      String key = string();
      skipWhitespace();
      if (peek() != ':') {
        throw error("expected ':'");
      }
      pos++;
      skipWhitespace();
      map.put(key, value(depth));
      skipWhitespace();
      char c = peek();
      pos++;
      if (c == '}') {
        return map;
      }
      if (c != ',') {
        throw error("expected ',' or '}'");
      }
    }
  }

  private List<Object> array(int depth) {
    if (depth > MAX_DEPTH) {
      throw error("nested too deeply");
    }
    pos++;
    List<Object> list = new ArrayList<>();
    skipWhitespace();
    if (peek() == ']') {
      pos++;
      return list;
    }
    while (true) {
      skipWhitespace();
      list.add(value(depth));
      skipWhitespace();
      char c = peek();
      pos++;
      if (c == ']') {
        return list;
      }
      if (c != ',') {
        throw error("expected ',' or ']'");
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
    char e = peek();
    pos++;
    switch (e) {
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
      boolean ascii = (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
      if (digit < 0 || !ascii) {
        throw error("bad unicode escape");
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
      throw error("bad number");
    }
    char first = text.charAt(pos);
    if (first == '0') {
      pos++;
    } else if (first >= '1' && first <= '9') {
      digits();
    } else {
      throw error("bad number");
    }
    if (pos < text.length() && text.charAt(pos) == '.') {
      pos++;
      if (digits() == 0) {
        throw error("bad number");
      }
    }
    if (pos < text.length() && (text.charAt(pos) == 'e' || text.charAt(pos) == 'E')) {
      pos++;
      if (pos < text.length() && (text.charAt(pos) == '+' || text.charAt(pos) == '-')) {
        pos++;
      }
      if (digits() == 0) {
        throw error("bad number");
      }
    }
    return Double.valueOf(Double.parseDouble(text.substring(start, pos)));
  }

  private int digits() {
    int count = 0;
    while (pos < text.length() && text.charAt(pos) >= '0' && text.charAt(pos) <= '9') {
      pos++;
      count++;
    }
    return count;
  }
}
