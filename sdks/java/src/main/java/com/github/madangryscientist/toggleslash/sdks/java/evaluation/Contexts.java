package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.util.LinkedHashMap;
import java.util.Map;

/** Context kinds and the two context shapes (single and multi). */
final class Contexts {
  /** What a kind that is read and absent means, everywhere. */
  static final String DEFAULT_KIND = "user";

  private Contexts() {}

  /** The key and attributes of one kind; {@code keyJson} is the key as JSON. */
  record KindContext(String key, Json keyJson, Json.Obj attributes) {}

  /** A kind is ASCII letters, digits, dot, underscore and hyphen, and is not kind or multi. */
  static boolean isKind(String text) {
    if (text.isEmpty() || text.equals("kind") || text.equals("multi")) {
      return false;
    }
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
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

  /** A property that is present and not null; null and absent are the same. */
  private static Json get(Json.Obj obj, String name) {
    Json value = obj.get(name);
    return value == null || value instanceof Json.Null ? null : value;
  }

  /**
   * Reads a context.
   *
   * @param context the context as JSON
   * @return kind to key and attributes, or null when the context is not valid
   */
  static Map<String, KindContext> normalise(Json context) {
    if (!(context instanceof Json.Obj obj)) {
      return null;
    }
    String kind = DEFAULT_KIND;
    Json rawKind = get(obj, "kind");
    if (rawKind != null) {
      if (!(rawKind instanceof Json.Str s)) {
        return null;
      }
      kind = s.value();
      if (kind.equals("multi")) {
        return readMulti(obj);
      }
      if (!isKind(kind)) {
        return null;
      }
    }
    KindContext entry = readEntry(obj);
    if (entry == null) {
      return null;
    }
    Map<String, KindContext> one = new LinkedHashMap<>();
    one.put(kind, entry);
    return one;
  }

  private static KindContext readEntry(Json.Obj value) {
    if (!(get(value, "key") instanceof Json.Str rawKey) || rawKey.value().isEmpty()) {
      return null;
    }
    Json attributes = get(value, "attributes");
    if (attributes != null && !(attributes instanceof Json.Obj)) {
      return null;
    }
    return new KindContext(rawKey.value(), rawKey, (Json.Obj) attributes);
  }

  private static Map<String, KindContext> readMulti(Json.Obj context) {
    if (get(context, "key") != null || get(context, "attributes") != null) {
      return null;
    }
    if (!(get(context, "contexts") instanceof Json.Obj contexts)) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<String, Json> property : contexts.fields().entrySet()) {
      if (!isKind(property.getKey())
          || !(property.getValue() instanceof Json.Obj value)
          || get(value, "kind") != null) {
        return null;
      }
      KindContext entry = readEntry(value);
      if (entry == null) {
        return null;
      }
      normalised.put(property.getKey(), entry);
    }
    return normalised.isEmpty() ? null : normalised;
  }

  /**
   * The bucket for the key of a kind: the key as it is for user, {@code kind:key} for any other
   * kind, and 0 when the context does not have that kind (no hash is computed).
   */
  static int bucket(String key, String salt, String kind, Map<String, KindContext> contexts) {
    String chosen = kind == null ? DEFAULT_KIND : kind;
    KindContext context = contexts.get(chosen);
    if (context == null) {
      return 0;
    }
    String contextKey = chosen.equals(DEFAULT_KIND) ? context.key() : chosen + ":" + context.key();
    return Bucketing.bucket(key, salt, contextKey);
  }
}
