package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.JsonException;
import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * An evaluation context: a single context ({@code key}, optional {@code kind} and {@code
 * attributes}) or a multi context ({@code kind: "multi"} and {@code contexts}), exactly as {@code
 * docs/evaluation.md} defines them. A value that is not valid is not an exception: the evaluation
 * answers {@code ERROR INVALID_CONTEXT} and the caller gets its fallback.
 */
public final class Context {
  private final Object value;

  private Context(Object value) {
    this.value = value;
  }

  /**
   * A context from a native map. Values may be {@code Boolean}, any {@code Number} (read as the
   * nearest {@code double}), any {@code CharSequence}, a {@code Map} with {@code String} keys, a
   * {@code Collection} or null. Anything else makes the context invalid.
   *
   * @param map the context as a map
   * @return the context, never null
   */
  public static Context of(Map<String, ?> map) {
    return new Context(convert(map, 0));
  }

  /**
   * A context from raw JSON text. It keeps a number such as {@code 1e999} and a lone surrogate
   * escape as the spec reads them. Text that is not JSON is an invalid context.
   *
   * @param json the JSON text
   * @return the context, never null
   */
  public static Context parse(String json) {
    try {
      return new Context(json == null ? null : Json.parse(json));
    } catch (JsonException e) {
      return new Context(null);
    }
  }

  /**
   * A {@code user} context with a key.
   *
   * @param key the key
   * @return the context, never null
   */
  public static Context user(String key) {
    return user(key, null);
  }

  /**
   * A {@code user} context with a key and attributes.
   *
   * @param key the key
   * @param attributes the attributes, or null; see {@link #of(Map)} for the values
   * @return the context, never null
   */
  public static Context user(String key, Map<String, ?> attributes) {
    Map<String, Object> map = new LinkedHashMap<>();
    map.put("key", key);
    if (attributes != null) {
      map.put("attributes", attributes);
    }
    return of(map);
  }

  /**
   * The value in the JSON model the evaluator reads: null, Boolean, Double, String, List or Map.
   */
  Object value() {
    return value;
  }

  /**
   * The nesting past which a native value is refused, so a cyclic map cannot overflow the stack.
   */
  private static final int MAX_DEPTH = 512;

  private static Object convert(Object input, int depth) {
    if (depth > MAX_DEPTH) {
      throw new IllegalArgumentException("too deep");
    }
    if (input == null || input instanceof Boolean || input instanceof String) {
      return input;
    }
    if (input instanceof Number) {
      return ((Number) input).doubleValue();
    }
    if (input instanceof CharSequence) {
      return input.toString();
    }
    if (input instanceof Map<?, ?>) {
      Map<String, Object> out = new LinkedHashMap<>();
      for (Map.Entry<?, ?> entry : ((Map<?, ?>) input).entrySet()) {
        if (!(entry.getKey() instanceof String)) {
          throw new IllegalArgumentException("key");
        }
        out.put((String) entry.getKey(), convert(entry.getValue(), depth + 1));
      }
      return out;
    }
    if (input instanceof Collection<?>) {
      List<Object> out = new ArrayList<>();
      for (Object item : (Collection<?>) input) {
        out.add(convert(item, depth + 1));
      }
      return out;
    }
    throw new IllegalArgumentException("type");
  }

  /** An invalid context is never described, and a valid one never prints what it holds. */
  @Override
  public String toString() {
    return "Context";
  }
}
