package com.github.madangryscientist.toggleslash.sdks.java.eval;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;

import org.junit.jupiter.api.Test;

/** The WHATWG decoding examples, and the encoder's lone surrogate. */
class Utf8Test {
  private static byte[] bytes(int... values) {
    byte[] out = new byte[values.length];
    for (int i = 0; i < values.length; i++) {
      out[i] = (byte) values[i];
    }
    return out;
  }

  private static final String BAD = "\uFFFD";

  @Test
  void decodesValidSequences() {
    assertEquals(
        "a\u00e9\u20ac\ud83d\ude00",
        Utf8.decode(bytes(0x61, 0xc3, 0xa9, 0xe2, 0x82, 0xac, 0xf0, 0x9f, 0x98, 0x80)));
  }

  @Test
  void aTruncatedSequenceIsOneReplacement() {
    assertEquals(BAD, Utf8.decode(bytes(0xe2, 0x82)));
    assertEquals(BAD + "A", Utf8.decode(bytes(0xe2, 0x82, 0x41)));
    assertEquals(BAD, Utf8.decode(bytes(0xf0, 0x9f, 0x98)));
  }

  @Test
  void anInvalidByteIsOneReplacementEach() {
    assertEquals(BAD + BAD, Utf8.decode(bytes(0xc0, 0x80)));
    assertEquals(BAD + BAD + BAD, Utf8.decode(bytes(0xed, 0xa0, 0x80)));
    assertEquals(BAD + BAD + BAD + BAD, Utf8.decode(bytes(0xf4, 0x90, 0x80, 0x80)));
    assertEquals(BAD, Utf8.decode(bytes(0xff)));
    assertEquals(BAD + BAD, Utf8.decode(bytes(0xf5, 0x80)));
    assertEquals(BAD, Utf8.decode(bytes(0x80)));
  }

  @Test
  void overlongAndMaximalSubpartRules() {
    assertEquals(BAD + BAD + BAD, Utf8.decode(bytes(0xe0, 0x80, 0x80)));
    assertEquals(BAD + "a", Utf8.decode(bytes(0xe1, 0x80, 0x61)));
    assertEquals(BAD + BAD + BAD + BAD, Utf8.decode(bytes(0xf0, 0x80, 0x80, 0x80)));
  }

  @Test
  void aSplitCharacterDecodesWhole() {
    Utf8.Decoder decoder = new Utf8.Decoder();
    StringBuilder out = new StringBuilder();
    byte[] data = bytes(0xe2, 0x82, 0xac);
    decoder.decode(data, 0, 1, out);
    assertEquals("", out.toString());
    decoder.decode(data, 1, 2, out);
    decoder.finish(out);
    assertEquals("\u20ac", out.toString());
  }

  @Test
  void aLeadingByteOrderMarkIsDropped() {
    assertEquals("x", Utf8.decode(bytes(0xef, 0xbb, 0xbf, 0x78)));
  }

  @Test
  void encodesALoneSurrogateAsAReplacement() {
    assertArrayEquals(bytes(0xef, 0xbf, 0xbd), Utf8.encode("\ud800"));
    assertArrayEquals(bytes(0xef, 0xbf, 0xbd, 0x61), Utf8.encode("\udc00a"));
    assertArrayEquals(bytes(0xf0, 0x9f, 0x98, 0x80), Utf8.encode("\ud83d\ude00"));
    assertArrayEquals(bytes(0xc3, 0xa9), Utf8.encode("\u00e9"));
  }
}
