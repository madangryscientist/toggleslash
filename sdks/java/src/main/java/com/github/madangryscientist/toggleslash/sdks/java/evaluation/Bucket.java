package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** Bucketing to {@code docs/evaluation.md}: SHA-256 of {@code <key>.<salt>.<contextKey>}. */
public final class Bucket {
  /** The number of buckets. */
  public static final int COUNT = 100000;

  private Bucket() {}

  /**
   * The first four bytes of the digest as a big-endian unsigned integer.
   *
   * @param key the flag or segment key
   * @param salt the salt
   * @param contextKey the context key as hashed
   * @return the unsigned value
   */
  public static long uint32(String key, String salt, String contextKey) {
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

  /**
   * The bucket, 0 to 99999.
   *
   * @param key the flag or segment key
   * @param salt the salt
   * @param contextKey the context key as hashed
   * @return the bucket
   */
  public static int of(String key, String salt, String contextKey) {
    return (int) (uint32(key, salt, contextKey) % COUNT);
  }
}
