package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** The bucket of a context key, to "Bucketing" in the evaluation spec. */
final class Bucket {

  /** Buckets run from 0 to 99999. */
  static final int COUNT = 100_000;

  private Bucket() {}

  /**
   * SHA-256 of {@code "<key>.<salt>.<contextKey>"}, first four bytes big-endian, as an unsigned
   * int.
   */
  static long uint32(String key, String salt, String contextKey) {
    byte[] hash = sha256(utf8(key + "." + salt + "." + contextKey));
    return ((hash[0] & 0xFFL) << 24)
        | ((hash[1] & 0xFFL) << 16)
        | ((hash[2] & 0xFFL) << 8)
        | (hash[3] & 0xFFL);
  }

  /** The bucket, 0 to 99999. */
  static int of(String key, String salt, String contextKey) {
    return (int) (uint32(key, salt, contextKey) % COUNT);
  }

  private static byte[] sha256(byte[] bytes) {
    try {
      return MessageDigest.getInstance("SHA-256").digest(bytes);
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 is part of every Java runtime", e);
    }
  }

  /**
   * UTF-8 with a lone surrogate written as U+FFFD. The JDK's own encoder writes {@code ?} for one,
   * which would hash differently.
   */
  static byte[] utf8(String text) {
    byte[] out = new byte[text.length() * 3];
    int n = 0;
    int i = 0;
    while (i < text.length()) {
      char c = text.charAt(i);
      int cp = c;
      if (Character.isHighSurrogate(c)
          && i + 1 < text.length()
          && Character.isLowSurrogate(text.charAt(i + 1))) {
        cp = Character.toCodePoint(c, text.charAt(i + 1));
        i += 2;
      } else {
        i++;
        if (Character.isSurrogate(c)) {
          cp = 0xFFFD;
        }
      }
      if (cp < 0x80) {
        out[n++] = (byte) cp;
      } else if (cp < 0x800) {
        out[n++] = (byte) (0xC0 | (cp >> 6));
        out[n++] = (byte) (0x80 | (cp & 0x3F));
      } else if (cp < 0x10000) {
        out[n++] = (byte) (0xE0 | (cp >> 12));
        out[n++] = (byte) (0x80 | ((cp >> 6) & 0x3F));
        out[n++] = (byte) (0x80 | (cp & 0x3F));
      } else {
        out[n++] = (byte) (0xF0 | (cp >> 18));
        out[n++] = (byte) (0x80 | ((cp >> 12) & 0x3F));
        out[n++] = (byte) (0x80 | ((cp >> 6) & 0x3F));
        out[n++] = (byte) (0x80 | (cp & 0x3F));
      }
    }
    return java.util.Arrays.copyOf(out, n);
  }
}
