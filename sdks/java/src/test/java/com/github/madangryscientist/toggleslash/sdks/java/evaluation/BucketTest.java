package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;

import java.nio.charset.StandardCharsets;
import org.junit.jupiter.api.Test;

class BucketTest {

  @Test
  void encodesALoneSurrogateAsReplacementCharacter() {
    byte[] replacement = "\ufffd".getBytes(StandardCharsets.UTF_8);
    assertArrayEquals(replacement, Bucket.utf8("\ud800"));
    assertArrayEquals(replacement, Bucket.utf8("\udc00"));
    assertArrayEquals(
        ("a" + "\ufffd" + "b").getBytes(StandardCharsets.UTF_8), Bucket.utf8("a\udc00b"));
  }

  @Test
  void encodesAPairAsOneFourByteSequence() {
    assertArrayEquals("\ud83d\ude00".getBytes(StandardCharsets.UTF_8), Bucket.utf8("\ud83d\ude00"));
    // A low surrogate before a high one is two lone surrogates, not a pair.
    assertArrayEquals("\ufffd\ufffd".getBytes(StandardCharsets.UTF_8), Bucket.utf8("\ude00\ud83d"));
  }

  @Test
  void aLoneSurrogateHashesAsReplacementCharacter() {
    assertEquals(Bucket.uint32("f", "s", "\ufffd"), Bucket.uint32("f", "s", "\ud800"));
  }
}
