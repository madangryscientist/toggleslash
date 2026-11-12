package com.github.madangryscientist.toggleslash.sdks.java.json;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** A strict recursive descent JSON parser (RFC 8259), written by hand. */
final class JsonParser {
  /** Nesting deeper than this is refused; the contract needs at least 512. */
  static final int MAX_DEPTH = 1000;

  private final String text;
  private int pos;

  private JsonParser(String text) {
    this.text = text;
  }

  static Json parse(String text) {
    try {
      JsonParser parser = new JsonParser(text);
      parser.skipSpace();
      Json value = parser.value(0);
      parser.skipSpace();
      if (parser.pos != text.length()) {
        throw parser.error("trailing characters");
      }
      return value;
    } catch (StackOverflowError e) {
      throw new JsonException("nested too deeply");
    }
  }

  private JsonException error(String what) {
    return new JsonException(what + " at " + pos);
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

  private Json value(int depth) {
    if (pos >= text.length()) {
      throw error("unexpected end");
    }
    char c = text.charAt(pos);
    switch (c) {
      case '{':
        return object(depth);
      case '[':
        return array(depth);
      case '"':
        return new Json.Str(string());
      case 't':
        literal("true");
        return Json.TRUE;
      case 'f':
        literal("false");
        return Json.FALSE;
      case 'n':
        literal("null");
        return Json.NULL;
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

  private Json object(int depth) {
    if (depth >= MAX_DEPTH) {
      throw error("nested too deeply");
    }
    pos++;
    Map<String, Json> fields = new LinkedHashMap<>();
    skipSpace();
    if (pos < text.length() && text.charAt(pos) == '}') {
      pos++;
      return new Json.Obj(fields);
    }
    while (true) {
      skipSpace();
      if (pos >= text.length() || text.charAt(pos) != '"') {
        throw error("expected a name");
      }
      String name = string();
      skipSpace();
      if (pos >= text.length() || text.charAt(pos) != ':') {
        throw error("expected a colon");
      }
      pos++;
      skipSpace();
      fields.put(name, value(depth + 1));
      skipSpace();
      if (pos >= text.length()) {
        throw error("unexpected end");
      }
      char c = text.charAt(pos++);
      if (c == '}') {
        return new Json.Obj(fields);
      }
      if (c != ',') {
        throw error("expected a comma");
      }
    }
  }

  private Json array(int depth) {
    if (depth >= MAX_DEPTH) {
      throw error("nested too deeply");
    }
    pos++;
    List<Json> items = new ArrayList<>();
    skipSpace();
    if (pos < text.length() && text.charAt(pos) == ']') {
      pos++;
      return new Json.Arr(items);
    }
    while (true) {
      skipSpace();
      items.add(value(depth + 1));
      skipSpace();
      if (pos >= text.length()) {
        throw error("unexpected end");
      }
      char c = text.charAt(pos++);
      if (c == ']') {
        return new Json.Arr(items);
      }
      if (c != ',') {
        throw error("expected a comma");
      }
    }
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
      if (pos >= text.length()) {
        throw error("unterminated string");
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
          out.append((char) hex4());
          break;
        default:
          throw error("bad escape");
      }
      start = pos;
    }
  }

  private int hex4() {
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
    return value;
  }

  private Json number() {
    int start = pos;
    if (text.charAt(pos) == '-') {
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
    return new Json.Num(Double.parseDouble(text.substring(start, pos)));
  }

  private int digits() {
    int start = pos;
    while (pos < text.length() && text.charAt(pos) >= '0' && text.charAt(pos) <= '9') {
      pos++;
    }
    return pos - start;
  }
}
