package com.github.madangryscientist.toggleslash.sdks.java.json;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;

import org.junit.jupiter.api.Test;

class Utf8Test {
  private static byte[] bytes(int... values) {
    byte[] out = new byte[values.length];
    for (int i = 0; i < values.length; i++) {
      out[i] = (byte) values[i];
    }
    return out;
  }

  @Test
  void decodesValid() {
    assertEquals(
        "aé€😀", Utf8.decode(bytes(0x61, 0xC3, 0xA9, 0xE2, 0x82, 0xAC, 0xF0, 0x9F, 0x98, 0x80)));
  }

  @Test
  void oneReplacementPerMaximalSubpart() {
    assertEquals("���", Utf8.decode(bytes(0xF0, 0x80, 0x80)));
    assertEquals("�", Utf8.decode(bytes(0xE0, 0xA0)));
    assertEquals("���A", Utf8.decode(bytes(0xED, 0xA0, 0x80, 0x41)).replace("A", "A"));
    assertEquals("����", Utf8.decode(bytes(0xC0, 0xAF, 0xFF, 0xF5)));
    assertEquals("a�b", Utf8.decode(bytes(0x61, 0xE2, 0x82, 0x62)));
  }

  @Test
  void streamingDecoderKeepsStateAcrossChunks() {
    StringBuilder out = new StringBuilder();
    Utf8.Decoder decoder = new Utf8.Decoder();
    byte[] data = bytes(0xF0, 0x9F, 0x98, 0x80, 0xE2, 0x82);
    for (byte b : data) {
      decoder.decode(new byte[] {b}, 0, 1, out);
    }
    decoder.finish(out);
    assertEquals("😀�", out.toString());
  }

  @Test
  void encodesLoneSurrogateAsReplacement() {
    assertArrayEquals(bytes(0xEF, 0xBF, 0xBD), Utf8.encode("\uD800"));
    assertArrayEquals(bytes(0xF0, 0x9F, 0x98, 0x80), Utf8.encode("😀"));
  }
}
