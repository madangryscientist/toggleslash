package com.github.madangryscientist.toggleslash.sdks.java.eval;

import java.lang.reflect.Array;
import java.util.ArrayList;
import java.util.IdentityHashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A valid context as a map from kind to that kind's key and attributes. The input is any Java
 * value: a map of strings, booleans, numbers, lists, arrays and maps. It is converted once into the
 * plain JSON tree the evaluator reads, in which every number is a {@code Double}.
 */
final class Contexts {
  static final String DEFAULT_KIND = "user";
  private static final int MAX_DEPTH = 1000;

  /**
   * A value of a type the evaluator does not know: present, never equal to anything, not a record.
   */
  enum Unsupported {
    VALUE
  }

  /** The key and attributes of one kind; attributes is null when absent. */
  record KindContext(String key, Map<String, Object> attributes) {}

  private final Map<String, KindContext> byKind;

  private Contexts(Map<String, KindContext> byKind) {
    this.byKind = byKind;
  }

  KindContext get(String kind) {
    return byKind.get(kind);
  }

  Iterable<Map.Entry<String, KindContext>> entries() {
    return byKind.entrySet();
  }

  /** Converts an arbitrary Java value into the JSON tree. */
  static Object convert(Object value) {
    return convert(value, 0, new IdentityHashMap<>());
  }

  private static Object convert(Object value, int depth, IdentityHashMap<Object, Boolean> path) {
    if (value == null) {
      return null;
    }
    if (value instanceof String || value instanceof Boolean) {
      return value;
    }
    if (value instanceof Number number) {
      return number.doubleValue();
    }
    if (value instanceof CharSequence text) {
      return text.toString();
    }
    boolean container =
        value instanceof Map || value instanceof Iterable || value.getClass().isArray();
    if (!container || depth >= MAX_DEPTH || path.put(value, Boolean.TRUE) != null) {
      return Unsupported.VALUE;
    }
    try {
      if (value instanceof Map<?, ?> map) {
        Map<String, Object> out = new LinkedHashMap<>();
        for (Map.Entry<?, ?> entry : map.entrySet()) {
          if (entry.getKey() instanceof String key) {
            out.put(key, convert(entry.getValue(), depth + 1, path));
          }
        }
        return out;
      }
      List<Object> out = new ArrayList<>();
      if (value instanceof Iterable<?> items) {
        for (Object item : items) {
          out.add(convert(item, depth + 1, path));
        }
      } else {
        int length = Array.getLength(value);
        for (int i = 0; i < length; i++) {
          out.add(convert(Array.get(value, i), depth + 1, path));
        }
      }
      return out;
    } finally {
      path.remove(value);
    }
  }

  private static KindContext readEntry(Map<String, Object> value) {
    Object key = value.get("key");
    Object attributes = value.get("attributes");
    if (!(key instanceof String k) || k.isEmpty()) {
      return null;
    }
    if (Values.isAbsent(attributes)) {
      return new KindContext(k, null);
    }
    return Values.isRecord(attributes) ? new KindContext(k, Values.record(attributes)) : null;
  }

  private static Contexts readMulti(Map<String, Object> value) {
    Object contexts = value.get("contexts");
    if (!Values.isAbsent(value.get("key"))
        || !Values.isAbsent(value.get("attributes"))
        || !Values.isRecord(contexts)) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<String, Object> entry : Values.record(contexts).entrySet()) {
      Object raw = entry.getValue();
      if (!Values.isKind(entry.getKey())
          || !Values.isRecord(raw)
          || !Values.isAbsent(Values.record(raw).get("kind"))) {
        return null;
      }
      KindContext read = readEntry(Values.record(raw));
      if (read == null) {
        return null;
      }
      normalised.put(entry.getKey(), read);
    }
    return normalised.isEmpty() ? null : new Contexts(normalised);
  }

  /**
   * Normalises a context.
   *
   * @param input any Java value
   * @return the contexts, or null when it is not a valid context
   */
  static Contexts normalise(Object input) {
    Object value = convert(input);
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    Object kind = map.get("kind");
    if (kind == null) {
      kind = DEFAULT_KIND;
    }
    if ("multi".equals(kind)) {
      return readMulti(map);
    }
    if (!Values.isKind(kind)) {
      return null;
    }
    KindContext entry = readEntry(map);
    return entry == null ? null : new Contexts(Map.of((String) kind, entry));
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
