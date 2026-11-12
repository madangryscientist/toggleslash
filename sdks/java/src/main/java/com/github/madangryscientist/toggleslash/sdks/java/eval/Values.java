package com.github.madangryscientist.toggleslash.sdks.java.eval;

import java.util.List;
import java.util.Map;

/**
 * Checks over the plain-JDK JSON tree, as the validation of {@code docs/evaluation.md} defines
 * them.
 */
final class Values {
  static final int BUCKET_COUNT = 100_000;

  private Values() {}

  static boolean isRecord(Object value) {
    return value instanceof Map;
  }

  static boolean isAbsent(Object value) {
    return value == null;
  }

  @SuppressWarnings("unchecked")
  static Map<String, Object> record(Object value) {
    return (Map<String, Object>) value;
  }

  @SuppressWarnings("unchecked")
  static List<Object> list(Object value) {
    return (List<Object>) value;
  }

  /** {@code ^[A-Za-z0-9._-]+$}, and neither {@code kind} nor {@code multi}. */
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

  /** A whole number that is not negative. */
  static boolean isCount(Object value) {
    return value instanceof Double d && !d.isInfinite() && d == Math.floor(d) && d >= 0;
  }

  static boolean isVariationIndex(Object value, int variationCount) {
    return isCount(value) && (Double) value < variationCount;
  }

  static boolean isStringArray(Object value) {
    if (!(value instanceof List<?> items)) {
      return false;
    }
    for (Object item : items) {
      if (!(item instanceof String)) {
        return false;
      }
    }
    return true;
  }

  static boolean isNegate(Object value) {
    return isAbsent(value) || value instanceof Boolean;
  }

  /** A count as an int, saturating. */
  static int toInt(Object count) {
    double d = (Double) count;
    return d >= Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) d;
  }
}
