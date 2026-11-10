package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.JsonException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;

/**
 * Who a flag is evaluated for: a single context (a {@code key}, an optional {@code kind} that
 * defaults to {@code user}, and optional {@code attributes}) or a multi context. A context is
 * immutable. One that is not valid is still a context: evaluating with it gives {@code ERROR
 * INVALID_CONTEXT} and never throws.
 */
public final class Context {
  private final Object tree;

  private Context(Object tree) {
    this.tree = tree;
  }

  /**
   * A context from the contract's shape: a map with {@code key}, and optionally {@code kind} and
   * {@code attributes} (or {@code kind: "multi"} and {@code contexts}). Numbers become doubles,
   * collections lists and any other value its {@code toString()}; a null map is an invalid context.
   */
  public static Context of(Map<String, ?> shape) {
    return new Context(shape == null ? null : copy(shape, 0));
  }

  /** A context from JSON text. Text that is not JSON is an invalid context, not an error. */
  public static Context fromJson(String json) {
    if (json == null) {
      return new Context(null);
    }
    try {
      return new Context(Json.parse(json));
    } catch (JsonException e) {
      return new Context(null);
    }
  }

  /** A {@code user} context with this key. */
  public static Context user(String key) {
    return kind("user", key);
  }

  /** A context of this kind with this key. */
  public static Context kind(String kind, String key) {
    Map<String, Object> shape = new LinkedHashMap<>();
    shape.put("kind", kind);
    shape.put("key", key);
    return new Context(shape);
  }

  /** The multi context of these single contexts, each under its own kind. */
  public static Context multi(Context... contexts) {
    Map<String, Object> entries = new LinkedHashMap<>();
    if (contexts != null) {
      for (Context context : contexts) {
        if (context != null && context.tree instanceof Map<?, ?> single) {
          Map<String, Object> entry = new LinkedHashMap<>();
          entry.put("key", single.get("key"));
          if (single.get("attributes") != null) {
            entry.put("attributes", single.get("attributes"));
          }
          Object kind = single.get("kind");
          entries.put(kind instanceof String text ? text : "user", entry);
        }
      }
    }
    Map<String, Object> shape = new LinkedHashMap<>();
    shape.put("kind", "multi");
    shape.put("contexts", entries);
    return new Context(shape);
  }

  /**
   * A copy of this context with one more attribute (on a single context; a multi one is unchanged).
   */
  public Context with(String attribute, Object value) {
    if (!(tree instanceof Map<?, ?> single) || "multi".equals(single.get("kind"))) {
      return this;
    }
    Map<String, Object> shape = new LinkedHashMap<>();
    for (Map.Entry<?, ?> entry : single.entrySet()) {
      shape.put((String) entry.getKey(), entry.getValue());
    }
    Map<String, Object> attributes = new LinkedHashMap<>();
    if (single.get("attributes") instanceof Map<?, ?> existing) {
      for (Map.Entry<?, ?> entry : existing.entrySet()) {
        attributes.put((String) entry.getKey(), entry.getValue());
      }
    }
    attributes.put(Objects.toString(attribute), copy(value, 0));
    shape.put("attributes", attributes);
    return new Context(shape);
  }

  /** The JSON tree the evaluator reads; null for an invalid context. Not for callers to change. */
  Object tree() {
    return tree;
  }

  /** The context as JSON text; {@code null} for an invalid context. */
  public String toJson() {
    return Json.write(tree);
  }

  @Override
  public String toString() {
    // A context holds end-user data, so its text form names nothing in it.
    return "Context";
  }

  private static Object copy(Object value, int depth) {
    if (depth > 64) {
      return null;
    }
    if (value == null || value instanceof String || value instanceof Boolean) {
      return value;
    }
    if (value instanceof Number number) {
      return number.doubleValue();
    }
    if (value instanceof Map<?, ?> map) {
      Map<String, Object> out = new LinkedHashMap<>();
      for (Map.Entry<?, ?> entry : map.entrySet()) {
        out.put(String.valueOf(entry.getKey()), copy(entry.getValue(), depth + 1));
      }
      return out;
    }
    if (value instanceof Iterable<?> items) {
      List<Object> out = new ArrayList<>();
      for (Object item : items) {
        out.add(copy(item, depth + 1));
      }
      return out;
    }
    if (value instanceof Object[] items) {
      List<Object> out = new ArrayList<>();
      for (Object item : items) {
        out.add(copy(item, depth + 1));
      }
      return out;
    }
    return String.valueOf(value);
  }
}
