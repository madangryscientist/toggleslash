package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.LinkedHashMap;
import java.util.Map;

/** A valid context as a map from kind to that kind's key and attributes. */
public final class Contexts {
  /** What a kind that is read and absent means, everywhere. */
  public static final String DEFAULT_KIND = "user";

  /**
   * The key and attributes of one kind.
   *
   * @param key the key
   * @param attributes the attributes, or null
   */
  public record KindContext(String key, Map<?, ?> attributes) {}

  private Contexts() {}

  private static KindContext readEntry(Object value) {
    Object key = Shape.get(value, "key");
    Object attributes = Shape.get(value, "attributes");
    if (!(key instanceof String k) || k.isEmpty()) {
      return null;
    }
    if (Shape.isAbsent(attributes)) {
      return new KindContext(k, null);
    }
    return Shape.isRecord(attributes) ? new KindContext(k, (Map<?, ?>) attributes) : null;
  }

  private static Map<String, KindContext> readMulti(Object value) {
    Object contexts = Shape.get(value, "contexts");
    if (!Shape.isAbsent(Shape.get(value, "key"))
        || !Shape.isAbsent(Shape.get(value, "attributes"))
        || !Shape.isRecord(contexts)) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<?, ?> entry : ((Map<?, ?>) contexts).entrySet()) {
      Object kind = entry.getKey();
      Object raw = entry.getValue();
      if (!Shape.isKind(kind) || !Shape.isRecord(raw) || !Shape.isAbsent(Shape.get(raw, "kind"))) {
        return null;
      }
      KindContext one = readEntry(raw);
      if (one == null) {
        return null;
      }
      normalised.put((String) kind, one);
    }
    return normalised.isEmpty() ? null : normalised;
  }

  /**
   * Normalises a context.
   *
   * @param value the context as the application gave it
   * @return the kinds it holds, or null when it is not a valid context
   */
  public static Map<String, KindContext> normalise(Object value) {
    if (!Shape.isRecord(value)) {
      return null;
    }
    Object kind = Shape.get(value, "kind");
    if (kind == null) {
      kind = DEFAULT_KIND;
    }
    if ("multi".equals(kind)) {
      return readMulti(value);
    }
    if (!Shape.isKind(kind)) {
      return null;
    }
    KindContext one = readEntry(value);
    if (one == null) {
      return null;
    }
    Map<String, KindContext> map = new LinkedHashMap<>();
    map.put((String) kind, one);
    return map;
  }

  /**
   * The bucket for the key of a kind: the key as it is for {@code user}, {@code <kind>:<key>} for
   * any other kind, and 0 when the context does not have that kind.
   *
   * @param key the flag or segment key
   * @param salt the salt
   * @param kind the kind, or null for {@code user}
   * @param contexts the context
   * @return the bucket
   */
  public static int kindBucket(
      String key, String salt, String kind, Map<String, KindContext> contexts) {
    String chosen = kind == null ? DEFAULT_KIND : kind;
    KindContext context = contexts.get(chosen);
    if (context == null) {
      return 0;
    }
    return Bucket.of(
        key, salt, chosen.equals(DEFAULT_KIND) ? context.key() : chosen + ":" + context.key());
  }
}
