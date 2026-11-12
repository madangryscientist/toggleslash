package com.github.madangryscientist.toggleslash.sdks.java;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** The bucket a context lands in for a flag or a segment rule. */
final class Bucketing {
  /** Buckets run 0 to 99999; a rollout weight of 1000 is one percent. */
  static final int BUCKET_COUNT = 100_000;

  private Bucketing() {}

  /**
   * SHA-256 of the UTF-8 bytes of {@code key.salt.contextKey}, the first four bytes as an unsigned
   * big-endian integer. A lone surrogate hashes as U+FFFD.
   *
   * @param key the flag's or segment's key
   * @param salt its salt
   * @param contextKey the context key to bucket by
   * @return the integer, 0 to 2^32 - 1
   */
  static long uint32(String key, String salt, String contextKey) {
    byte[] digest;
    try {
      digest =
          MessageDigest.getInstance("SHA-256")
              .digest(Utf8.encode(key + "." + salt + "." + contextKey));
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 is required of every JDK", e);
    }
    return ((digest[0] & 0xFFL) << 24)
        | ((digest[1] & 0xFFL) << 16)
        | ((digest[2] & 0xFFL) << 8)
        | (digest[3] & 0xFFL);
  }

  /**
   * The bucket: {@link #uint32} modulo 100000.
   *
   * @param key the flag's or segment's key
   * @param salt its salt
   * @param contextKey the context key to bucket by
   * @return a bucket from 0 to 99999
   */
  static int bucket(String key, String salt, String contextKey) {
    return (int) (uint32(key, salt, contextKey) % BUCKET_COUNT);
  }
}
