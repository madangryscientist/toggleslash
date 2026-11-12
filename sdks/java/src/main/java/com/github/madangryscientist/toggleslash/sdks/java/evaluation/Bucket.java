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
   * SHA-256 of the UTF-8 bytes of {@code <key>.<salt>.<contextKey>}, the first 4 bytes as a
   * big-endian uint32, modulo 100000. A lone surrogate is hashed as U+FFFD.
   */
  static int of(String key, String salt, String contextKey) {
    byte[] digest;
    try {
      digest =
          MessageDigest.getInstance("SHA-256")
              .digest(Utf8.encode(key + "." + salt + "." + contextKey));
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 is part of every Java platform", e);
    }
    long value =
        ((digest[0] & 0xFFL) << 24)
            | ((digest[1] & 0xFFL) << 16)
            | ((digest[2] & 0xFFL) << 8)
            | (digest[3] & 0xFFL);
    return (int) (value % COUNT);
  }
}
