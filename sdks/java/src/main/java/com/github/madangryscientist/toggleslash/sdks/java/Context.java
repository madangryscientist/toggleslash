package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Who a flag is evaluated for: a single context (a kind, a key and attributes) or a multi context
 * (one single context per kind). It wraps a {@link Json} object, so a context may carry any value
 * the evaluator can read. The SDK never sends one anywhere.
 */
public final class Context {
  private final Json json;

  private Context(Json json) {
    this.json = json;
  }

  /**
   * A single context of kind {@code user}.
   *
   * @param key the user's key
   * @return the context
   */
  public static Context user(String key) {
    return of("user", key, Map.of());
  }

  /**
   * A single context.
   *
   * @param kind the kind, such as {@code user} or {@code org}
   * @param key the key
   * @param attributes the attributes; values are null, Boolean, Number, String, List, Map or Json
   * @return the context
   * @throws IllegalArgumentException when an attribute value is of another class
   */
  public static Context of(String kind, String key, Map<String, ?> attributes) {
    Map<String, Json> fields = new LinkedHashMap<>();
    fields.put("kind", new Json.Str(String.valueOf(kind)));
    fields.put("key", new Json.Str(String.valueOf(key)));
    if (attributes != null && !attributes.isEmpty()) {
      fields.put("attributes", Json.of(attributes));
    }
    return new Context(new Json.Obj(fields));
  }

  /**
   * A multi context.
   *
   * @param contexts single contexts built with {@link #of(String, String, Map)}, one per kind
   * @return the context
   * @throws IllegalArgumentException when one is not a single context with a kind
   */
  public static Context multi(Context... contexts) {
    Map<String, Json> byKind = new LinkedHashMap<>();
    for (Context context : contexts) {
      if (!(context.json instanceof Json.Obj obj) || !(obj.get("kind") instanceof Json.Str kind)) {
        throw new IllegalArgumentException("a multi context takes single contexts with a kind");
      }
      Map<String, Json> rest = new LinkedHashMap<>(obj.fields());
      rest.remove("kind");
      byKind.put(kind.value(), new Json.Obj(rest));
    }
    Map<String, Json> fields = new LinkedHashMap<>();
    fields.put("kind", new Json.Str("multi"));
    fields.put("contexts", new Json.Obj(byKind));
    return new Context(new Json.Obj(fields));
  }

  /**
   * A context from its JSON form, exactly as given. One that is not valid evaluates to {@code
   * INVALID_CONTEXT}.
   *
   * @param json the context
   * @return the context
   */
  public static Context of(Json json) {
    return new Context(json == null ? Json.NULL : json);
  }

  /**
   * The JSON form.
   *
   * @return the context as JSON
   */
  public Json json() {
    return json;
  }

  @Override
  public String toString() {
    return "Context";
  }
}
