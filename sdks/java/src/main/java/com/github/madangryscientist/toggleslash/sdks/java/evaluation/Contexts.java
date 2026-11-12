package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.LinkedHashMap;
import java.util.Map;

/** A valid context as a map from kind to that kind's key and attributes, after normalising. */
final class Contexts {
  /** What a kind that is read and absent means, everywhere. */
  static final String DEFAULT_KIND = "user";

  /** The key and attributes of one kind. */
  record KindContext(String key, Map<?, ?> attributes) {}

  private final Map<String, KindContext> byKind;

  private Contexts(Map<String, KindContext> byKind) {
    this.byKind = byKind;
  }

  Iterable<Map.Entry<String, KindContext>> entries() {
    return byKind.entrySet();
  }

  KindContext get(String kind) {
    return byKind.get(kind);
  }

  /** {@code ^[A-Za-z0-9._-]+$}, and neither {@code kind} nor {@code multi}. */
  static boolean isKind(Object value) {
    if (!(value instanceof String)) {
      return false;
    }
    String text = (String) value;
    if (text.isEmpty() || text.equals("kind") || text.equals("multi")) {
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
    return true;
  }

  static boolean isRecord(Object value) {
    return value instanceof Map<?, ?>;
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
    return isRecord(attributes) ? new KindContext((String) key, (Map<?, ?>) attributes) : null;
  }

  private static Contexts readMulti(Map<?, ?> value) {
    Object contexts = value.get("contexts");
    if (value.get("key") != null || value.get("attributes") != null || !isRecord(contexts)) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<?, ?> entry : ((Map<?, ?>) contexts).entrySet()) {
      Object kind = entry.getKey();
      Object raw = entry.getValue();
      if (!isKind(kind) || !isRecord(raw) || ((Map<?, ?>) raw).get("kind") != null) {
        return null;
      }
      KindContext context = readEntry((Map<?, ?>) raw);
      if (context == null) {
        return null;
      }
      normalised.put((String) kind, context);
    }
    return normalised.isEmpty() ? null : new Contexts(normalised);
  }

  /** The context as a map from kind to key and attributes, or null when it is not valid. */
  static Contexts normalise(Object value) {
    if (!isRecord(value)) {
      return null;
    }
    Map<?, ?> record = (Map<?, ?>) value;
    Object kind = record.get("kind");
    if (kind == null) {
      kind = DEFAULT_KIND;
    }
    if ("multi".equals(kind)) {
      return readMulti(record);
    }
    if (!isKind(kind)) {
      return null;
    }
    KindContext entry = readEntry(record);
    if (entry == null) {
      return null;
    }
    Map<String, KindContext> single = new LinkedHashMap<>();
    single.put((String) kind, entry);
    return new Contexts(single);
  }

  /**
   * The bucket for the key of {@code kind} (null is {@code user}): the key as it is for {@code
   * user}, {@code <kind>:<key>} for any other kind, and 0 when the context does not have that kind.
   */
  int kindBucket(String key, String salt, String kind) {
    String chosen = kind == null ? DEFAULT_KIND : kind;
    KindContext context = byKind.get(chosen);
    if (context == null) {
      return 0;
    }
    return Bucket.of(
        key, salt, chosen.equals(DEFAULT_KIND) ? context.key() : chosen + ":" + context.key());
  }
}
