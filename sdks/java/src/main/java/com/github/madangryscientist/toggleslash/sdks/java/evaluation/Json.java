package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A strict JSON reader. A value is {@code null}, {@link Boolean}, {@link Double}, {@link String},
 * {@code List<Object>} or {@code Map<String, Object>}. The last of a repeated key wins, numbers are
 * binary64 (a number past the range is infinity), a lone surrogate escape is kept, and nesting over
 * {@value #MAX_DEPTH} levels is refused. The lists and maps it returns cannot be changed, so a
 * value an SDK hands to a caller cannot alter the ruleset it came from.
 */
public final class Json {
  /** The deepest nesting accepted. */
  public static final int MAX_DEPTH = 1000;

  private final String text;
  private int pos;

  private Json(String text) {
    this.text = text;
  }

  /** The error of text that is not JSON. */
  public static final class ParseException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    ParseException(String message) {
      super(message);
    }
  }

  /**
   * Parses one JSON document.
   *
   * @param text the document
   * @return the value
   * @throws ParseException when it is not JSON
   */
  public static Object parse(String text) {
    Json parser = new Json(text);
    parser.skipSpace();
    Object value = parser.value(0);
    parser.skipSpace();
    if (parser.pos != text.length()) {
      throw parser.error("unexpected data after the value");
    }
    return value;
  }

  private ParseException error(String message) {
    return new ParseException(message + " at " + pos);
  }

  private void skipSpace() {
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
      throw error("nesting too deep");
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
        throw error("expected a string key");
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

  private List<Object> array(int depth) {
    if (depth > MAX_DEPTH) {
      throw error("nesting too deep");
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
    StringBuilder out = null;
    int start = pos;
    while (true) {
      if (pos >= text.length()) {
        throw error("unterminated string");
      }
      char c = text.charAt(pos);
      if (c == '"') {
        String tail = text.substring(start, pos);
        pos++;
        return out == null ? tail : out.append(tail).toString();
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
        if (pos >= text.length()) {
          throw error("unterminated string");
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
            out.append(unicode());
            break;
          default:
            throw error("bad escape");
        }
        start = pos;
      } else {
        pos++;
      }
    }
  }

  private char unicode() {
    if (pos + 4 > text.length()) {
      throw error("bad unicode escape");
    }
    int v = 0;
    for (int i = 0; i < 4; i++) {
      int d = Character.digit(text.charAt(pos + i), 16);
      char ch = text.charAt(pos + i);
      boolean ascii =
          (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F');
      if (d < 0 || !ascii) {
        throw error("bad unicode escape");
      }
      v = v * 16 + d;
    }
    pos += 4;
    return (char) v;
  }

  private Double number() {
    int start = pos;
    if (pos < text.length() && text.charAt(pos) == '-') {
      pos++;
    }
    if (pos >= text.length()) {
      throw error("bad number");
    }
    char c = text.charAt(pos);
    if (c == '0') {
      pos++;
    } else if (c >= '1' && c <= '9') {
      digits();
    } else {
      throw error("unexpected character");
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
}
