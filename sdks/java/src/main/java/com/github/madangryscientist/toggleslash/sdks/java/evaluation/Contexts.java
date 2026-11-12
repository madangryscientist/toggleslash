package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.LinkedHashMap;
import java.util.Map;

/** Context reading, to "Context" in the evaluation spec. */
final class Contexts {

  /** What a kind that is read and absent means, everywhere. */
  static final String DEFAULT_KIND = "user";

  private Contexts() {}

  /** The key and attributes of one kind. {@code attributes} is null when there are none. */
  record KindContext(String key, Map<?, ?> attributes) {}

  /** {@code ^[A-Za-z0-9._-]+$}, and neither {@code kind} nor {@code multi}. */
  static boolean isKind(Object value) {
    if (!(value instanceof String text) || text.isEmpty()) {
      return false;
    }
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      boolean ok =
          (c >= 'A' && c <= 'Z')
              || (c >= 'a' && c <= 'z')
              || (c >= '0' && c <= '9')
              || c == '.'
              || c == '_'
              || c == '-';
      if (!ok) {
        return false;
      }
    }
    return !text.equals("kind") && !text.equals("multi");
  }

  /** The context as a map from kind to its key and attributes, or null when it is not valid. */
  static Map<String, KindContext> normalise(Object context) {
    if (!(context instanceof Map<?, ?> map)) {
      return null;
    }
    Object kind = map.get("kind");
    if (kind == null) {
      kind = DEFAULT_KIND;
    }
    if ("multi".equals(kind)) {
      return readMulti(map);
    }
    if (!isKind(kind)) {
      return null;
    }
    KindContext entry = readEntry(map);
    if (entry == null) {
      return null;
    }
    Map<String, KindContext> contexts = new LinkedHashMap<>();
    contexts.put((String) kind, entry);
    return contexts;
  }

  private static KindContext readEntry(Map<?, ?> map) {
    if (!(map.get("key") instanceof String key) || key.isEmpty()) {
      return null;
    }
    Object attributes = map.get("attributes");
    if (attributes == null) {
      return new KindContext(key, null);
    }
    return attributes instanceof Map<?, ?> found ? new KindContext(key, found) : null;
  }

  private static Map<String, KindContext> readMulti(Map<?, ?> map) {
    if (map.get("key") != null
        || map.get("attributes") != null
        || !(map.get("contexts") instanceof Map<?, ?> contexts)
        || contexts.isEmpty()) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<?, ?> item : contexts.entrySet()) {
      Object kind = item.getKey();
      if (!isKind(kind) || !(item.getValue() instanceof Map<?, ?> raw) || raw.get("kind") != null) {
        return null;
      }
      KindContext entry = readEntry(raw);
      if (entry == null) {
        return null;
      }
      normalised.put((String) kind, entry);
    }
    return normalised;
  }

  /**
   * The bucket for the key of {@code kind} (null is {@code user}): the key as it is for {@code
   * user}, {@code <kind>:<key>} for any other kind, and 0 when the context does not have that kind.
   */
  static int kindBucket(String key, String salt, String kind, Map<String, KindContext> contexts) {
    String chosen = kind == null ? DEFAULT_KIND : kind;
    KindContext context = contexts.get(chosen);
    if (context == null) {
      return 0;
    }
    String contextKey = chosen.equals(DEFAULT_KIND) ? context.key() : chosen + ":" + context.key();
    return Bucket.of(key, salt, contextKey);
  }
}
