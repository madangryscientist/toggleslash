package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;

import org.junit.jupiter.api.Test;

/** The UTF-8 decoder and encoder against the WHATWG Encoding standard's rules. */
class Utf8Test {
  private static byte[] bytes(int... values) {
    byte[] out = new byte[values.length];
    for (int i = 0; i < values.length; i++) {
      out[i] = (byte) values[i];
    }
    return out;
  }

  @Test
  void decodesValidText() {
    assertEquals(
        "a\u00e9\u20ac\ud83d\ude00",
        Utf8.decode(bytes(0x61, 0xc3, 0xa9, 0xe2, 0x82, 0xac, 0xf0, 0x9f, 0x98, 0x80)));
  }

  @Test
  void dropsAByteOrderMark() {
    assertEquals("a", Utf8.decode(bytes(0xef, 0xbb, 0xbf, 0x61)));
  }

  @Test
  void oneReplacementPerMaximalInvalidSubpart() {
    // Examples from the Encoding standard and the Unicode "U+FFFD" best practice.
    assertEquals("\ufffd\ufffd\ufffd", Utf8.decode(bytes(0xf0, 0x80, 0x80)));
    assertEquals("\ufffd\ufffd", Utf8.decode(bytes(0xc0, 0xaf)));
    assertEquals("\ufffd\ufffd", Utf8.decode(bytes(0xe0, 0x80)));
    // A truncated three-byte sequence is one replacement, not two.
    assertEquals("\ufffd", Utf8.decode(bytes(0xe2, 0x82)));
    // 0xE0 0x9F is not valid (overlong): 0xE0 is replaced, then 0x9F is replaced.
    assertEquals("\ufffd\ufffd", Utf8.decode(bytes(0xe0, 0x9f)));
    // A surrogate encoded in UTF-8 (ED A0 80) is three replacements.
    assertEquals("\ufffd\ufffd\ufffd", Utf8.decode(bytes(0xed, 0xa0, 0x80)));
    // A lead byte followed by a good ASCII byte keeps the ASCII byte.
    assertEquals("\ufffda", Utf8.decode(bytes(0xe2, 0x61)));
    assertEquals("\ufffd\ufffd\ufffd\ufffd", Utf8.decode(bytes(0xf4, 0x90, 0x80, 0x80)));
    assertEquals("\ufffd", Utf8.decode(bytes(0xff)));
  }

  @Test
  void aSequenceSplitAcrossChunksDecodesTheSame() {
    byte[] all = bytes(0x61, 0xf0, 0x9f, 0x98, 0x80, 0xe2, 0x82, 0xac);
    for (int split = 0; split <= all.length; split++) {
      Utf8.Decoder decoder = new Utf8.Decoder();
      StringBuilder out = new StringBuilder();
      decoder.decode(all, 0, split, out);
      decoder.decode(all, split, all.length - split, out);
      decoder.finish(out);
      assertEquals("a\ud83d\ude00\u20ac", out.toString(), "split at " + split);
    }
  }

  @Test
  void aTruncatedSequenceAtTheEndIsOneReplacement() {
    Utf8.Decoder decoder = new Utf8.Decoder();
    StringBuilder out = new StringBuilder();
    decoder.decode(bytes(0xf0, 0x9f, 0x98), 0, 3, out);
    decoder.finish(out);
    assertEquals("\ufffd", out.toString());
  }

  @Test
  void encodesALoneSurrogateAsReplacementNotAQuestionMark() {
    assertArrayEquals(bytes(0x61, 0xef, 0xbf, 0xbd, 0x62), Utf8.encode("a\ud800b"));
    assertArrayEquals(bytes(0xef, 0xbf, 0xbd), Utf8.encode("\udc00"));
    assertArrayEquals(bytes(0xf0, 0x9f, 0x98, 0x80), Utf8.encode("\ud83d\ude00"));
  }
}
