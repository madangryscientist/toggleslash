package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** A valid context as a map from kind to that kind's key and attributes. */
final class Contexts {
  /** What a kind that is read and absent means, everywhere. */
  static final String DEFAULT_KIND = "user";

  /** The key and attributes of one kind. */
  static final class KindContext {
    final String key;
    final Map<?, ?> attributes;

    KindContext(String key, Map<?, ?> attributes) {
      this.key = key;
      this.attributes = attributes;
    }
  }

  final Map<String, KindContext> byKind;

  private Contexts(Map<String, KindContext> byKind) {
    this.byKind = byKind;
  }

  private static KindContext readEntry(Map<?, ?> value) {
    Object key = value.get("key");
    Object attributes = value.get("attributes");
    if (!(key instanceof String) || ((String) key).isEmpty()) {
      return null;
    }
    if (attributes == null) {
      return new KindContext((String) key, null);
    }
    return attributes instanceof Map ? new KindContext((String) key, (Map<?, ?>) attributes) : null;
  }

  private static Contexts readMulti(Map<?, ?> value) {
    Object contexts = value.get("contexts");
    if (value.get("key") != null || value.get("attributes") != null || !(contexts instanceof Map)) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<?, ?> entry : ((Map<?, ?>) contexts).entrySet()) {
      Object kind = entry.getKey();
      Object raw = entry.getValue();
      if (!Validate.isKind(kind)
          || !(raw instanceof Map)
          || ((Map<?, ?>) raw).get("kind") != null) {
        return null;
      }
      KindContext one = readEntry((Map<?, ?>) raw);
      if (one == null) {
        return null;
      }
      normalised.put((String) kind, one);
    }
    return normalised.isEmpty() ? null : new Contexts(normalised);
  }

  /** The context as kinds, or {@code null} when it is not valid. */
  static Contexts normalise(Object value) {
    if (!(value instanceof Map)) {
      return null;
    }
    Map<?, ?> map = (Map<?, ?>) value;
    Object kind = map.get("kind");
    if (kind == null) {
      kind = DEFAULT_KIND;
    }
    if ("multi".equals(kind)) {
      return readMulti(map);
    }
    if (!Validate.isKind(kind)) {
      return null;
    }
    KindContext one = readEntry(map);
    if (one == null) {
      return null;
    }
    Map<String, KindContext> single = new LinkedHashMap<>();
    single.put((String) kind, one);
    return new Contexts(single);
  }

  /**
   * The bucket for the key of a kind (null is {@code user}): the key as it is for {@code user},
   * {@code <kind>:<key>} for any other kind, and 0 when the context does not have that kind.
   */
  int kindBucket(String key, String salt, String kind) {
    String chosen = kind == null ? DEFAULT_KIND : kind;
    KindContext context = byKind.get(chosen);
    if (context == null) {
      return 0;
    }
    return Bucket.bucket(
        key, salt, chosen.equals(DEFAULT_KIND) ? context.key : chosen + ":" + context.key);
  }

  /** Tells whether the context has, in any of its kinds, a key the lists hold for that kind. */
  boolean listedIn(Map<String, java.util.Set<String>> keys) {
    for (Map.Entry<String, KindContext> entry : byKind.entrySet()) {
      java.util.Set<String> held = keys.get(entry.getKey());
      if (held != null && held.contains(entry.getValue().key)) {
        return true;
      }
    }
    return false;
  }

  /** A value of a context attribute as the evaluator reads it: see {@link #normaliseValue}. */
  static boolean isListLike(Object value) {
    return value instanceof List || value instanceof Object[];
  }

  /**
   * Reads a caller's attribute value as a JSON value: a {@link String}, a {@link Boolean}, a {@link
   * Double} (any {@link Number}), a {@link List} (also an object array), a {@link Map}, or {@code
   * null} for {@code null} and for any other class.
   */
  static Object normaliseValue(Object value) {
    if (value instanceof String || value instanceof Boolean || value instanceof Map) {
      return value;
    }
    if (value instanceof Number) {
      return ((Number) value).doubleValue();
    }
    if (value instanceof List) {
      return value;
    }
    if (value instanceof Object[]) {
      return java.util.Arrays.asList((Object[]) value);
    }
    if (value instanceof CharSequence) {
      return value.toString();
    }
    return null;
  }
}
