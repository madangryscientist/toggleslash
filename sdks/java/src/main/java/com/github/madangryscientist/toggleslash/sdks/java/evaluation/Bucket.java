package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** Where a context lands for a flag or segment, to "Bucketing" in the evaluation spec. */
final class Bucket {
  /** Buckets run 0 to 99999; a rollout weight of 1000 is 1 percent. */
  static final int COUNT = 100_000;

  private Bucket() {}

  /**
   * The first 4 bytes of the SHA-256 of {@code <key>.<salt>.<contextKey>} as a big-endian uint32.
   */
  static long uint32(String key, String salt, String contextKey) {
    byte[] digest;
    try {
      digest =
          MessageDigest.getInstance("SHA-256")
              .digest(Utf8.encode(key + "." + salt + "." + contextKey));
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 is required of every Java runtime", e);
    }
    return ((digest[0] & 0xFFL) << 24)
        | ((digest[1] & 0xFFL) << 16)
        | ((digest[2] & 0xFFL) << 8)
        | (digest[3] & 0xFFL);
  }

  /** The bucket, 0 to 99999: the uint32 modulo 100000. */
  static int of(String key, String salt, String contextKey) {
    return (int) (uint32(key, salt, contextKey) % COUNT);
  }
}
