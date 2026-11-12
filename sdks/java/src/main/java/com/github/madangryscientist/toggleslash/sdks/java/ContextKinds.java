package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Context kinds and the two context shapes (single and multi), to "Context" in the evaluation spec.
 */
final class ContextKinds {
  /** What a kind that is read and absent means, everywhere. */
  static final String DEFAULT = "user";

  private ContextKinds() {}

  /** The key and attributes of one kind, after normalising. */
  record KindContext(String key, Map<?, ?> attributes) {}

  /**
   * Tells whether {@code text} is a kind: {@code ^[A-Za-z0-9._-]+$}, and neither {@code kind} nor
   * {@code multi}.
   */
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

  /**
   * Reads a context as a map from kind to its key and attributes.
   *
   * @param context the context: a map with string keys whose values are JSON values
   * @return the map, or null when the context is not valid
   */
  static Map<String, KindContext> normalise(Object context) {
    if (!(context instanceof Map)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) context;
    String kind = DEFAULT;
    Object rawKind = map.get("kind");
    if (rawKind != null) {
      if (!(rawKind instanceof CharSequence)) {
        return null;
      }
      kind = rawKind.toString();
      if (kind.equals("multi")) {
        return readMulti(map);
      }
      if (!isKind(kind)) {
        return null;
      }
    }
    KindContext entry = readEntry(map);
    if (entry == null) {
      return null;
    }
    Map<String, KindContext> result = new LinkedHashMap<>();
    result.put(kind, entry);
    return result;
  }

  /**
   * The bucket for the key of {@code kind} (null is {@code user}): the key as it is for {@code
   * user}, {@code kind:key} for any other kind, and 0 when the context does not have that kind.
   */
  static int bucket(String key, String salt, String kind, Map<String, KindContext> contexts) {
    String chosen = kind == null ? DEFAULT : kind;
    KindContext context = contexts.get(chosen);
    if (context == null) {
      return 0;
    }
    return Bucketing.bucket(
        key, salt, chosen.equals(DEFAULT) ? context.key() : chosen + ":" + context.key());
  }

  private static KindContext readEntry(Map<?, ?> value) {
    Object rawKey = value.get("key");
    if (!(rawKey instanceof CharSequence)) {
      return null;
    }
    String key = rawKey.toString();
    if (key.isEmpty()) {
      return null;
    }
    Object attributes = value.get("attributes");
    if (attributes != null && !(attributes instanceof Map)) {
      return null;
    }
    return new KindContext(key, (Map<?, ?>) attributes);
  }

  private static Map<String, KindContext> readMulti(Map<?, ?> context) {
    if (context.get("key") != null || context.get("attributes") != null) {
      return null;
    }
    Object contexts = context.get("contexts");
    if (!(contexts instanceof Map)) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<?, ?> property : ((Map<?, ?>) contexts).entrySet()) {
      if (!(property.getKey() instanceof String)) {
        return null;
      }
      String name = (String) property.getKey();
      Object value = property.getValue();
      if (!isKind(name) || !(value instanceof Map) || ((Map<?, ?>) value).get("kind") != null) {
        return null;
      }
      KindContext entry = readEntry((Map<?, ?>) value);
      if (entry == null) {
        return null;
      }
      normalised.put(name, entry);
    }
    return normalised.isEmpty() ? null : normalised;
  }

  /** A list view of an array-like value, or null when {@code value} is not one. */
  static List<?> asList(Object value) {
    if (value instanceof List) {
      return (List<?>) value;
    }
    if (value instanceof Object[]) {
      return java.util.Arrays.asList((Object[]) value);
    }
    return null;
  }
}
