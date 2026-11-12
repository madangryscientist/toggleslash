package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** Where a context lands for a flag or a segment. */
final class Bucket {
  static final int COUNT = 100_000;

  private Bucket() {}

  /**
   * SHA-256 of the UTF-8 bytes of {@code key.salt.contextKey}, the first four bytes as a big-endian
   * unsigned integer.
   */
  static long uint32(String key, String salt, String contextKey) {
    byte[] digest;
    try {
      digest =
          MessageDigest.getInstance("SHA-256")
              .digest(Utf8.encode(key + "." + salt + "." + contextKey));
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException(e);
    }
    return ((digest[0] & 0xFFL) << 24)
        | ((digest[1] & 0xFFL) << 16)
        | ((digest[2] & 0xFFL) << 8)
        | (digest[3] & 0xFFL);
  }

  /** The bucket, 0 to 99999. */
  static int of(String key, String salt, String contextKey) {
    return (int) (uint32(key, salt, contextKey) % COUNT);
  }
}
