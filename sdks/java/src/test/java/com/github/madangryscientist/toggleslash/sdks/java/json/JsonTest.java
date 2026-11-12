package com.github.madangryscientist.toggleslash.sdks.java.json;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

/** The JSON parser and writer, and the WHATWG UTF-8 codec under them. */
final class JsonTest {
  @Test
  void everyNumberIsADouble() {
    assertEquals(1.0, Json.parse("1"));
    assertEquals(Double.POSITIVE_INFINITY, Json.parse("1e999"));
    assertEquals(9007199254740992.0, Json.parse("9007199254740993"));
    assertEquals(-0.0, Json.parse("-0"));
  }

  @Test
  void aByteOrderMarkIsSkippedAndADuplicateKeyKeepsTheLastValue() {
    Object value = Json.parse("\uFEFF{\"a\":1,\"a\":2}");
    assertEquals(Map.of("a", 2.0), value);
  }

  @Test
  void aLoneSurrogateEscapeStaysALoneSurrogate() {
    assertEquals("\uD800", Json.parse("\"\\ud800\""));
    assertEquals("\"\\ud800\"", Json.write("\uD800"));
  }

  @Test
  void depthIsBoundedAndNotAStackOverflow() {
    String ok = "[".repeat(Json.MAX_DEPTH) + "]".repeat(Json.MAX_DEPTH);
    assertInstanceOf(List.class, Json.parse(ok));
    String deep = "[".repeat(Json.MAX_DEPTH + 1) + "]".repeat(Json.MAX_DEPTH + 1);
    assertThrows(JsonException.class, () -> Json.parse(deep));
  }

  @Test
  void malformedTextIsRejected() {
    for (String bad :
        List.of("", "{", "[1,]", "{\"a\":}", "01", "1.", "\"\\x\"", "tru", "[1] 2", "'a'")) {
      assertThrows(JsonException.class, () -> Json.parse(bad), bad);
    }
  }

  @Test
  void writeEscapesAndMapsNonFiniteNumbersToNull() {
    assertEquals("null", Json.write(Double.NaN));
    assertEquals("null", Json.write(Double.POSITIVE_INFINITY));
    assertEquals("\"a\\n\\\"b\\\\\"", Json.write("a\n\"b\\"));
    assertEquals(
        "{\"k\":[1,true,null,\"x\"]}",
        Json.write(Map.of("k", java.util.Arrays.asList(1.0, true, null, "x"))));
    assertThrows(IllegalArgumentException.class, () -> Json.write(new Object()));
  }

  @Test
  void utf8UsesOneReplacementPerMaximalInvalidSubpart() {
    assertEquals("\uFFFD\uFFFD", Utf8.decode(new byte[] {(byte) 0xC0, (byte) 0x80}));
    assertEquals("\uFFFD", Utf8.decode(new byte[] {(byte) 0xE2, (byte) 0x82}));
    assertEquals(
        "\uFFFD\uFFFD\uFFFD", Utf8.decode(new byte[] {(byte) 0xED, (byte) 0xA0, (byte) 0x80}));
    assertEquals(
        "\uFFFD\uFFFD\uFFFD\uFFFD",
        Utf8.decode(new byte[] {(byte) 0xF0, (byte) 0x80, (byte) 0x80, (byte) 0x80}));
    assertEquals("\u20AC", Utf8.decode(new byte[] {(byte) 0xE2, (byte) 0x82, (byte) 0xAC}));
    assertEquals(
        "\uD83D\uDE00",
        Utf8.decode(new byte[] {(byte) 0xF0, (byte) 0x9F, (byte) 0x98, (byte) 0x80}));
  }

  @Test
  void utf8DecodesAcrossPieces() {
    byte[] euro = {(byte) 0xE2, (byte) 0x82, (byte) 0xAC};
    Utf8 decoder = new Utf8();
    StringBuilder out = new StringBuilder();
    decoder.feed(euro, 0, 1, out);
    assertEquals("", out.toString());
    decoder.feed(euro, 1, 2, out);
    decoder.finish(out);
    assertEquals("\u20AC", out.toString());
  }

  @Test
  void utf8EncodesALoneSurrogateAsReplacement() {
    assertEquals(3, Utf8.encode("\uD800").length);
    assertEquals("\uFFFD", Utf8.decode(Utf8.encode("\uD800")));
    assertTrue(Utf8.encode("a\u00E9\u20AC\uD83D\uDE00").length == 1 + 2 + 3 + 4);
  }
}
