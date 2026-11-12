package com.github.madangryscientist.toggleslash.sdks.java.eval;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** Where a context lands for a flag or segment. */
final class Bucket {
  static final int COUNT = 100_000;

  private Bucket() {}

  /**
   * SHA-256 of the UTF-8 bytes of {@code <key>.<salt>.<contextKey>}, the first 4 bytes as a
   * big-endian uint32, modulo 100000.
   */
  static int of(String key, String salt, String contextKey) {
    byte[] digest;
    try {
      digest =
          MessageDigest.getInstance("SHA-256")
              .digest(Utf8.encode(key + "." + salt + "." + contextKey));
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException(e);
    }
    long value =
        ((digest[0] & 0xffL) << 24)
            | ((digest[1] & 0xffL) << 16)
            | ((digest[2] & 0xffL) << 8)
            | (digest[3] & 0xffL);
    return (int) (value % COUNT);
  }
}
