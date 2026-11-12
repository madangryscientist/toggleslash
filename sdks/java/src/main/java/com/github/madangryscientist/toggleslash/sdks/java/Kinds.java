package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** The kind rules of contract section 4: which served values an accessor accepts, and as what. */
final class Kinds {
  /** What an accessor asks for. */
  enum Want {
    BOOL,
    STRING,
    FLOAT,
    INT,
    JSON,
    ANY,
    NONE
  }

  /** Returned by {@link #convert} when the served value is of the wrong kind. */
  static final Object MISMATCH = new Object();

  private static final double TWO_63 = 9.223372036854775808E18;

  private Kinds() {}

  /** The accessor a generic call means, from its fallback. */
  static Want wantOf(Object fallback) {
    if (fallback == null) {
      return Want.ANY;
    }
    if (fallback instanceof Boolean) {
      return Want.BOOL;
    }
    if (fallback instanceof String) {
      return Want.STRING;
    }
    if (fallback instanceof Long
        || fallback instanceof Integer
        || fallback instanceof Short
        || fallback instanceof Byte) {
      return Want.INT;
    }
    if (fallback instanceof Double || fallback instanceof Float) {
      return Want.FLOAT;
    }
    if (fallback instanceof Map || fallback instanceof List) {
      return Want.JSON;
    }
    return Want.NONE;
  }

  /**
   * Checks a served value against what the accessor wants.
   *
   * @param want the accessor
   * @param served the value from the evaluator (a plain JSON tree)
   * @param fallback the caller's fallback
   * @param type the integer or float class to produce, or null
   * @return the value as the caller gets it, or {@link #MISMATCH}
   */
  static Object convert(Want want, Object served, Object fallback, Class<?> type) {
    switch (want) {
      case BOOL:
        return served instanceof Boolean ? served : MISMATCH;
      case STRING:
        return served instanceof String ? served : MISMATCH;
      case FLOAT:
        if (!(served instanceof Double d)) {
          return MISMATCH;
        }
        return type == Float.class ? (Object) Float.valueOf(d.floatValue()) : d;
      case INT:
        return integer(served, type);
      case JSON:
        return fallback == null || kindOf(served).equals(kindOf(fallback))
            ? freeze(served)
            : MISMATCH;
      case ANY:
        return freeze(served);
      default:
        return MISMATCH;
    }
  }

  private static Object integer(Object served, Class<?> type) {
    if (!(served instanceof Double d) || d.isNaN() || d.isInfinite() || d != Math.rint(d)) {
      return MISMATCH;
    }
    if (type == Integer.class) {
      return d >= Integer.MIN_VALUE && d <= Integer.MAX_VALUE ? (Object) d.intValue() : MISMATCH;
    }
    if (type == Short.class) {
      return d >= Short.MIN_VALUE && d <= Short.MAX_VALUE ? (Object) d.shortValue() : MISMATCH;
    }
    if (type == Byte.class) {
      return d >= Byte.MIN_VALUE && d <= Byte.MAX_VALUE ? (Object) d.byteValue() : MISMATCH;
    }
    return d >= -TWO_63 && d < TWO_63 ? (Object) d.longValue() : MISMATCH;
  }

  /** The JSON kind of a Java value, or null when it has none (and for an absent fallback). */
  static String kindOf(Object value) {
    if (value == null) {
      return "null";
    }
    if (value instanceof Boolean) {
      return "boolean";
    }
    if (value instanceof CharSequence) {
      return "string";
    }
    if (value instanceof Number) {
      return "number";
    }
    if (value instanceof Collection || value.getClass().isArray()) {
      return "array";
    }
    if (value instanceof Map) {
      return "object";
    }
    return null;
  }

  /** A read-only deep view of a served tree, so a caller cannot change the shared ruleset. */
  static Object freeze(Object value) {
    if (value instanceof List<?> list) {
      List<Object> copy = new ArrayList<>(list.size());
      for (Object item : list) {
        copy.add(freeze(item));
      }
      return Collections.unmodifiableList(copy);
    }
    if (value instanceof Map<?, ?> map) {
      Map<String, Object> copy = new LinkedHashMap<>();
      for (Map.Entry<?, ?> entry : map.entrySet()) {
        copy.put((String) entry.getKey(), freeze(entry.getValue()));
      }
      return Collections.unmodifiableMap(copy);
    }
    return value;
  }
}
