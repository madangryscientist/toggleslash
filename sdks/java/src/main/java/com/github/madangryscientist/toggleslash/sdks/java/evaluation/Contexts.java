package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.LinkedHashMap;
import java.util.Map;

/** A valid context as a map from kind to that kind's key and attributes. */
final class Contexts {
  static final String DEFAULT_KIND = "user";

  private Contexts() {}

  /** The key and attributes of one kind, after normalising. */
  record KindContext(String key, Map<?, ?> attributes) {}

  private static KindContext readEntry(Object value) {
    Object key = Js.member(value, "key");
    Object attributes = Js.member(value, "attributes");
    if (!(key instanceof String) || ((String) key).isEmpty()) {
      return null;
    }
    if (attributes == null) {
      return new KindContext((String) key, null);
    }
    return Js.isRecord(attributes) ? new KindContext((String) key, (Map<?, ?>) attributes) : null;
  }

  private static Map<String, KindContext> readMulti(Object value) {
    Object contexts = Js.member(value, "contexts");
    if (Js.member(value, "key") != null
        || Js.member(value, "attributes") != null
        || !Js.isRecord(contexts)) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<?, ?> entry : ((Map<?, ?>) contexts).entrySet()) {
      Object kind = entry.getKey();
      Object raw = entry.getValue();
      if (!Validate.isKind(kind) || !Js.isRecord(raw) || Js.member(raw, "kind") != null) {
        return null;
      }
      KindContext read = readEntry(raw);
      if (read == null) {
        return null;
      }
      normalised.put((String) kind, read);
    }
    return normalised.isEmpty() ? null : normalised;
  }

  /** The context as a map from kind to key and attributes, or null when it is not valid. */
  static Map<String, KindContext> normalise(Object value) {
    if (!Js.isRecord(value)) {
      return null;
    }
    Object kind = Js.member(value, "kind");
    if (kind == null) {
      kind = DEFAULT_KIND;
    }
    if ("multi".equals(kind)) {
      return readMulti(value);
    }
    if (!Validate.isKind(kind)) {
      return null;
    }
    KindContext entry = readEntry(value);
    if (entry == null) {
      return null;
    }
    Map<String, KindContext> out = new LinkedHashMap<>();
    out.put((String) kind, entry);
    return out;
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
    return Bucket.of(
        key, salt, chosen.equals(DEFAULT_KIND) ? context.key() : chosen + ":" + context.key());
  }
}
