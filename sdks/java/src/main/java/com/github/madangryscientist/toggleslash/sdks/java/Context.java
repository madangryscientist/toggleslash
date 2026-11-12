package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Builds the context maps the client takes: a convenience, since any {@code Map} with the right
 * keys works.
 */
public final class Context {
  private Context() {}

  /**
   * A single-kind context of the default kind {@code user}.
   *
   * @param key the context key
   * @return the context
   */
  public static Map<String, Object> of(String key) {
    Map<String, Object> context = new LinkedHashMap<>();
    context.put("key", key);
    return context;
  }

  /**
   * A single-kind context with attributes.
   *
   * @param key the context key
   * @param attributes the attributes, each a JSON value
   * @return the context
   */
  public static Map<String, Object> of(String key, Map<String, Object> attributes) {
    Map<String, Object> context = of(key);
    context.put("attributes", attributes);
    return context;
  }

  /**
   * A single-kind context of a given kind.
   *
   * @param kind the context kind, such as {@code organization}
   * @param key the context key
   * @param attributes the attributes, each a JSON value
   * @return the context
   */
  public static Map<String, Object> of(String kind, String key, Map<String, Object> attributes) {
    Map<String, Object> context = of(key, attributes);
    context.put("kind", kind);
    return context;
  }
}
