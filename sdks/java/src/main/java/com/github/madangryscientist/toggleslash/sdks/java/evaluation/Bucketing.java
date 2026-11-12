package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/** Where a context lands for a flag or segment. */
public final class Bucketing {
  /** Buckets run 0 to 99999; a rollout weight of 1000 is one per cent. */
  static final int BUCKET_COUNT = 100_000;

  private Bucketing() {}

  /**
   * SHA-256 of the UTF-8 bytes of {@code key.salt.contextKey}, the first four bytes as a big-endian
   * unsigned integer, modulo 100000. A lone surrogate hashes as U+FFFD.
   *
   * @param key the flag or segment key
   * @param salt its salt
   * @param contextKey the context's key
   * @return the bucket
   */
  public static int bucket(String key, String salt, String contextKey) {
    MessageDigest digest;
    try {
      digest = MessageDigest.getInstance("SHA-256");
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 is part of every Java runtime", e);
    }
    byte[] hash = digest.digest(Utf8.encode(key + "." + salt + "." + contextKey));
    long value =
        ((hash[0] & 0xFFL) << 24)
            | ((hash[1] & 0xFFL) << 16)
            | ((hash[2] & 0xFFL) << 8)
            | (hash[3] & 0xFFL);
    return (int) (value % BUCKET_COUNT);
  }
}
