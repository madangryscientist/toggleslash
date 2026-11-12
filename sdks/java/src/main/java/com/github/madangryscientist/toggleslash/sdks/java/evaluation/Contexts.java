package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.LinkedHashMap;
import java.util.Map;

/** A valid context as a map from kind to that kind's key and attributes. */
final class Contexts {
  /** What a kind that is read and absent means, everywhere. */
  static final String DEFAULT_KIND = "user";

  /** The key and attributes of one kind; {@code attributes} is null when there are none. */
  record KindContext(String key, Map<?, ?> attributes) {}

  private Contexts() {}

  private static KindContext readEntry(Map<?, ?> value) {
    Object key = value.get("key");
    Object attributes = value.get("attributes");
    if (!Values.isKey(key)) {
      return null;
    }
    if (Values.isAbsent(attributes)) {
      return new KindContext((String) key, null);
    }
    return attributes instanceof Map<?, ?> record ? new KindContext((String) key, record) : null;
  }

  private static Map<String, KindContext> readMulti(Map<?, ?> value) {
    Object contexts = value.get("contexts");
    if (!Values.isAbsent(value.get("key"))
        || !Values.isAbsent(value.get("attributes"))
        || !(contexts instanceof Map<?, ?> all)) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<?, ?> e : all.entrySet()) {
      if (!Values.isKind(e.getKey())
          || !(e.getValue() instanceof Map<?, ?> raw)
          || !Values.isAbsent(raw.get("kind"))) {
        return null;
      }
      KindContext entry = readEntry(raw);
      if (entry == null) {
        return null;
      }
      normalised.put((String) e.getKey(), entry);
    }
    return normalised.isEmpty() ? null : normalised;
  }

  /** The context as a map from kind to key and attributes, or null when it is not valid. */
  static Map<String, KindContext> normalise(Object value) {
    if (!(value instanceof Map<?, ?> record)) {
      return null;
    }
    Object kind = record.get("kind");
    if (kind == null) {
      kind = DEFAULT_KIND;
    }
    if ("multi".equals(kind)) {
      return readMulti(record);
    }
    if (!Values.isKind(kind)) {
      return null;
    }
    KindContext entry = readEntry(record);
    if (entry == null) {
      return null;
    }
    Map<String, KindContext> single = new LinkedHashMap<>();
    single.put((String) kind, entry);
    return single;
  }

  /**
   * The bucket for the key of a kind (null is user): the key as it is for user, {@code
   * <kind>:<key>} for any other kind, and 0 when the context does not have that kind.
   */
  static int kindBucket(String key, String salt, String kind, Map<String, KindContext> contexts) {
    String chosen = kind == null ? DEFAULT_KIND : kind;
    KindContext context = contexts.get(chosen);
    if (context == null) {
      return 0;
    }
    return Bucket.of(
        key, salt, chosen.equals(DEFAULT_KIND) ? context.key() : chosen + ":" + context.key());
  }
}
