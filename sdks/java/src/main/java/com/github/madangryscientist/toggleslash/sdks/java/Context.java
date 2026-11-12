package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * A typed way to build a context. A plain {@link Map} tree works as a context too.
 *
 * <p>The SDK never sends a context anywhere.
 */
public final class Context {
  private final Map<String, Object> value;

  private Context(Map<String, Object> value) {
    this.value = value;
  }

  /**
   * A single context of kind {@code user}.
   *
   * @param key the key
   * @return the context
   */
  public static Context user(String key) {
    return of("user", key);
  }

  /**
   * A single context.
   *
   * @param kind the kind
   * @param key the key
   * @return the context
   */
  public static Context of(String kind, String key) {
    Map<String, Object> map = new LinkedHashMap<>();
    map.put("kind", kind);
    map.put("key", key);
    return new Context(map);
  }

  /**
   * A copy of a single context with one more attribute. A number of any type is the JSON number of
   * the same value.
   *
   * @param name the attribute name
   * @param attribute the value: null, a Boolean, a Number, a String, a List or a Map
   * @return the new context
   */
  public Context with(String name, Object attribute) {
    Map<String, Object> copy = new LinkedHashMap<>(value);
    Object existing = copy.get("attributes");
    Map<String, Object> attributes = new LinkedHashMap<>();
    if (existing instanceof Map) {
      for (Map.Entry<?, ?> entry : ((Map<?, ?>) existing).entrySet()) {
        attributes.put((String) entry.getKey(), entry.getValue());
      }
    }
    attributes.put(name, attribute);
    copy.put("attributes", attributes);
    return new Context(copy);
  }

  /**
   * A multi context.
   *
   * @param contexts single contexts, each of its own kind
   * @return the context
   */
  public static Context multi(Context... contexts) {
    Map<String, Object> map = new LinkedHashMap<>();
    Map<String, Object> inner = new LinkedHashMap<>();
    for (Context context : contexts) {
      Map<String, Object> single = new LinkedHashMap<>(context.value);
      Object kind = single.remove("kind");
      inner.put(String.valueOf(kind), single);
    }
    map.put("kind", "multi");
    map.put("contexts", inner);
    return new Context(map);
  }

  /**
   * The context as the evaluator reads it.
   *
   * @return an unmodifiable view
   */
  public Map<String, Object> toMap() {
    return java.util.Collections.unmodifiableMap(value);
  }
}
