package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.List;
import java.util.Map;

/** Tests on the plain Java values a parsed JSON text is made of. */
final class Values {
  private Values() {}

  static boolean isRecord(Object value) {
    return value instanceof Map;
  }

  static boolean isAbsent(Object value) {
    return value == null;
  }

  static Object get(Object record, String name) {
    return ((Map<?, ?>) record).get(name);
  }

  /** {@code ^[A-Za-z0-9._-]+$}, and neither {@code kind} nor {@code multi}. */
  static boolean isKind(Object value) {
    if (!(value instanceof String s) || s.isEmpty() || s.equals("kind") || s.equals("multi")) {
      return false;
    }
    for (int i = 0; i < s.length(); i++) {
      char c = s.charAt(i);
      boolean ok =
          (c >= 'A' && c <= 'Z')
              || (c >= 'a' && c <= 'z')
              || Strings.isDigit(c)
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

  /** A non-negative integer; a number like 1.0 counts, as in the spec. */
  static boolean isCount(Object value) {
    if (!(value instanceof Number n)) {
      return false;
    }
    double d = n.doubleValue();
    return Double.isFinite(d) && d == Math.floor(d) && d >= 0;
  }

  static boolean isVariationIndex(Object value, int variationCount) {
    return isCount(value) && ((Number) value).doubleValue() < variationCount;
  }

  static int asInt(Object count) {
    return (int) ((Number) count).doubleValue();
  }

  static boolean isStringList(Object value) {
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

  static boolean isNegate(Object value) {
    return isAbsent(value) || value instanceof Boolean;
  }

  /** A string, a finite number or a boolean. */
  static boolean isClauseValue(Object value) {
    if (value instanceof Number n) {
      return Double.isFinite(n.doubleValue());
    }
    return value instanceof String || value instanceof Boolean;
  }
}
