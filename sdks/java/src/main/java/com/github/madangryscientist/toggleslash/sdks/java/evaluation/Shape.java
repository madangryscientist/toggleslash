package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/** Shape checks on parsed JSON, shared by the ruleset and the context. */
final class Shape {
  private Shape() {}

  static boolean isRecord(Object value) {
    return value instanceof Map;
  }

  static boolean isAbsent(Object value) {
    return value == null;
  }

  /** Reads a property of a record; null for a missing one. */
  static Object get(Object record, String name) {
    return ((Map<?, ?>) record).get(name);
  }

  /** {@code ^[A-Za-z0-9._-]+$}, ASCII only, and neither {@code kind} nor {@code multi}. */
  static boolean isKind(Object value) {
    if (!(value instanceof String s) || s.isEmpty() || s.equals("kind") || s.equals("multi")) {
      return false;
    }
    for (int i = 0; i < s.length(); i++) {
      char c = s.charAt(i);
      boolean ok =
          (c >= 'a' && c <= 'z')
              || (c >= 'A' && c <= 'Z')
              || (c >= '0' && c <= '9')
              || c == '.'
              || c == '_'
              || c == '-';
      if (!ok) {
        return false;
      }
    }
    return true;
  }

  static boolean isOptionalKind(Object value) {
    return isAbsent(value) || isKind(value);
  }

  static boolean isKey(Object value) {
    return value instanceof String s && !s.isEmpty();
  }

  /** A finite integer, 0 or more. */
  static boolean isCount(Object value) {
    return value instanceof Double d
        && !d.isInfinite()
        && !d.isNaN()
        && d == Math.rint(d)
        && d >= 0;
  }

  static boolean isStringArray(Object value) {
    if (!(value instanceof List<?> list)) {
      return false;
    }
    for (Object item : list) {
      if (!(item instanceof String)) {
        return false;
      }
    }
    return true;
  }

  /** Splits on a character and keeps every empty part, trailing ones too. */
  static List<String> split(String text, char separator) {
    List<String> parts = new ArrayList<>();
    int start = 0;
    while (true) {
      int at = text.indexOf(separator, start);
      if (at < 0) {
        parts.add(text.substring(start));
        return parts;
      }
      parts.add(text.substring(start, at));
      start = at + 1;
    }
  }

  /**
   * An attribute reference such as {@code /address/city}: the components after dropping the leading
   * slash, with {@code ~1} read as {@code /} and {@code ~0} as {@code ~}. Null for an empty
   * component or a {@code ~} followed by anything else.
   */
  static List<String> parseReference(String text) {
    List<String> components = new ArrayList<>();
    for (String part : split(text.substring(1), '/')) {
      if (part.isEmpty()) {
        return null;
      }
      StringBuilder decoded = new StringBuilder();
      for (int i = 0; i < part.length(); i++) {
        char c = part.charAt(i);
        if (c != '~') {
          decoded.append(c);
          continue;
        }
        char next = i + 1 < part.length() ? part.charAt(i + 1) : '\0';
        if (next == '0') {
          decoded.append('~');
        } else if (next == '1') {
          decoded.append('/');
        } else {
          return null;
        }
        i++;
      }
      components.add(decoded.toString());
    }
    return components;
  }
}
