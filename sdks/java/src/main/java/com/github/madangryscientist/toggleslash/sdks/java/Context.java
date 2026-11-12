package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Helpers that build evaluation contexts as plain maps. A context is any {@code Map<String,
 * Object>} the evaluation spec accepts; these only save typing.
 */
public final class Context {
  private Context() {}

  /**
   * A user context.
   *
   * @param key the context key
   * @return a context of kind {@code user}
   */
  public static Map<String, Object> of(String key) {
    return of("user", key);
  }

  /**
   * A single-kind context.
   *
   * @param kind the kind
   * @param key the context key
   * @return the context
   */
  public static Map<String, Object> of(String kind, String key) {
    Map<String, Object> context = new LinkedHashMap<>();
    context.put("kind", kind);
    context.put("key", key);
    return context;
  }

  /**
   * A copy of a single-kind context with one more attribute.
   *
   * @param context a context made by {@link #of(String)} or {@link #of(String, String)}
   * @param name the attribute name
   * @param value the attribute value
   * @return a new context
   */
  @SuppressWarnings("unchecked")
  public static Map<String, Object> with(Map<String, Object> context, String name, Object value) {
    Map<String, Object> copy = new LinkedHashMap<>(context);
    Object existing = copy.get("attributes");
    Map<String, Object> attributes =
        existing instanceof Map<?, ?> m
            ? new LinkedHashMap<>((Map<String, Object>) m)
            : new LinkedHashMap<>();
    attributes.put(name, value);
    copy.put("attributes", attributes);
    return copy;
  }

  /**
   * A multi-kind context from single-kind contexts.
   *
   * @param parts single-kind contexts; each one's {@code kind} names its entry
   * @return the multi-kind context
   */
  @SafeVarargs
  public static Map<String, Object> multi(Map<String, Object>... parts) {
    Map<String, Object> contexts = new LinkedHashMap<>();
    for (Map<String, Object> part : parts) {
      Object kind = part.get("kind");
      Map<String, Object> entry = new LinkedHashMap<>();
      entry.put("key", part.get("key"));
      if (part.get("attributes") != null) {
        entry.put("attributes", part.get("attributes"));
      }
      contexts.put(kind == null ? "user" : kind.toString(), entry);
    }
    Map<String, Object> multi = new LinkedHashMap<>();
    multi.put("kind", "multi");
    multi.put("contexts", contexts);
    return multi;
  }
}
