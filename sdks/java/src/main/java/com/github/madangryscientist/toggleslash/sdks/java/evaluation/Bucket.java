package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** Where a context lands for a flag or segment. */
final class Bucket {
  /** Buckets run 0 to 99999; a rollout weight of 1000 is 1 percent. */
  static final int COUNT = 100_000;

  private Bucket() {}

  /**
   * The first four bytes of SHA-256 over the UTF-8 bytes of {@code <key>.<salt>.<contextKey>}, as a
   * big-endian unsigned integer. A lone surrogate hashes as U+FFFD.
   */
  static long uint32(String key, String salt, String contextKey) {
    byte[] input = Utf8.encode(key + "." + salt + "." + contextKey);
    byte[] digest;
    try {
      digest = MessageDigest.getInstance("SHA-256").digest(input);
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 is part of every Java runtime", e);
    }
    return ((digest[0] & 0xFFL) << 24)
        | ((digest[1] & 0xFFL) << 16)
        | ((digest[2] & 0xFFL) << 8)
        | (digest[3] & 0xFFL);
  }

  /** The bucket: {@link #uint32} modulo 100000. */
  static int bucket(String key, String salt, String contextKey) {
    return (int) (uint32(key, salt, contextKey) % COUNT);
  }
}
