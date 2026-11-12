package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.List;
import java.util.Map;

/**
 * Reads of JSON-shaped values. A JSON object is a {@link Map}, an array a {@link List}, and null
 * and an absent member are the same thing. Any {@link Number} counts as a JSON number and is read
 * as a binary64 value.
 */
final class Js {
  private Js() {}

  static boolean isRecord(Object value) {
    return value instanceof Map;
  }

  /**
   * The member {@code name} of a record, or null when it is missing. The caller checked the type.
   */
  static Object member(Object record, String name) {
    return ((Map<?, ?>) record).get(name);
  }

  static boolean isNumber(Object value) {
    return value instanceof Number;
  }

  static double number(Object value) {
    return ((Number) value).doubleValue();
  }

  static boolean isFinite(Object value) {
    return value instanceof Number && Double.isFinite(((Number) value).doubleValue());
  }

  static boolean isArray(Object value) {
    return value instanceof List;
  }

  static List<?> array(Object value) {
    return (List<?>) value;
  }
}
