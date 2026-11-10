package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Context normalisation and bucketing, to "Contexts" and "Buckets" in {@code docs/evaluation.md}.
 */
final class Contexts {
  static final String DEFAULT_KIND = "user";

  /** The key and attributes of one kind, after normalising. Attributes is null when absent. */
  record KindContext(String key, Map<?, ?> attributes) {}

  private Contexts() {}

  private static KindContext readEntry(Map<?, ?> value) {
    Object key = value.get("key");
    Object attributes = value.get("attributes");
    if (!(key instanceof String text) || text.isEmpty()) {
      return null;
    }
    if (attributes == null) {
      return new KindContext(text, null);
    }
    return attributes instanceof Map<?, ?> map ? new KindContext(text, map) : null;
  }

  private static Map<String, KindContext> readMulti(Map<?, ?> value) {
    Object contexts = value.get("contexts");
    if (value.get("key") != null
        || value.get("attributes") != null
        || !(contexts instanceof Map<?, ?> entries)) {
      return null;
    }
    Map<String, KindContext> normalised = new LinkedHashMap<>();
    for (Map.Entry<?, ?> entry : entries.entrySet()) {
      Object kind = entry.getKey();
      if (!Validate.isKind(kind)
          || !(entry.getValue() instanceof Map<?, ?> raw)
          || raw.get("kind") != null) {
        return null;
      }
      KindContext context = readEntry(raw);
      if (context == null) {
        return null;
      }
      normalised.put((String) kind, context);
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
    if (!Validate.isKind(kind)) {
      return null;
    }
    KindContext entry = readEntry(record);
    return entry == null ? null : Map.of((String) kind, entry);
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
    return bucket(key, salt, contextKey);
  }

  /**
   * SHA-256 of the UTF-8 bytes of {@code <key>.<salt>.<contextKey>}, the first 4 bytes as a
   * big-endian uint32, modulo 100000.
   */
  static int bucket(String key, String salt, String contextKey) {
    long value = uint32(key, salt, contextKey);
    return (int) (value % Validate.BUCKET_COUNT);
  }

  /** The first four bytes of the digest as an unsigned big-endian integer. */
  static long uint32(String key, String salt, String contextKey) {
    byte[] digest = sha256(utf8(key + "." + salt + "." + contextKey));
    return ((digest[0] & 0xffL) << 24)
        | ((digest[1] & 0xffL) << 16)
        | ((digest[2] & 0xffL) << 8)
        | (digest[3] & 0xffL);
  }

  private static byte[] sha256(byte[] input) {
    try {
      return MessageDigest.getInstance("SHA-256").digest(input);
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 is required of every Java platform", e);
    }
  }

  /** UTF-8, with a lone surrogate as U+FFFD (Java's own encoder would write a question mark). */
  static byte[] utf8(String text) {
    ByteArrayOutputStream out = new ByteArrayOutputStream(text.length() + 8);
    byte[] replacement = "�".getBytes(StandardCharsets.UTF_8);
    int i = 0;
    while (i < text.length()) {
      char c = text.charAt(i);
      if (Character.isHighSurrogate(c)
          && i + 1 < text.length()
          && Character.isLowSurrogate(text.charAt(i + 1))) {
        out.writeBytes(text.substring(i, i + 2).getBytes(StandardCharsets.UTF_8));
        i += 2;
      } else if (Character.isSurrogate(c)) {
        out.writeBytes(replacement);
        i++;
      } else {
        int start = i;
        while (i < text.length() && !Character.isSurrogate(text.charAt(i))) {
          i++;
        }
        out.writeBytes(text.substring(start, i).getBytes(StandardCharsets.UTF_8));
      }
    }
    return out.toByteArray();
  }
}
