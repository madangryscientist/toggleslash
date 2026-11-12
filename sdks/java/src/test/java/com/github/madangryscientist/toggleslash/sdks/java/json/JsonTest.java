package com.github.madangryscientist.toggleslash.sdks.java.json;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

/** The JSON and UTF-8 rules the contract names. */
class JsonTest {
  private static Object parse(byte[] bytes) {
    return Json.parse(bytes, 0, bytes.length);
  }

  @Test
  void numbersAreBinary64() {
    assertEquals(Double.POSITIVE_INFINITY, Json.parse("1e999"));
    assertEquals(Double.NEGATIVE_INFINITY, Json.parse("-1e999"));
    assertEquals(9007199254740992.0, Json.parse("9007199254740993"));
    assertEquals(
        Long.toString(Double.doubleToRawLongBits(-0.0)),
        Long.toString(Double.doubleToRawLongBits((Double) Json.parse("-0.0"))));
    assertEquals(2.0, Json.parse("2.0"));
  }

  @Test
  void duplicateKeysKeepTheLastValue() {
    Map<?, ?> map = (Map<?, ?>) Json.parse("{\"a\":1,\"a\":2}");
    assertEquals(1, map.size());
    assertEquals(2.0, map.get("a"));
  }

  @Test
  void aByteOrderMarkIsSkippedAndBadUtf8IsReplaced() {
    byte[] bom = {(byte) 0xEF, (byte) 0xBB, (byte) 0xBF, '[', '1', ']'};
    assertEquals(List.of(1.0), parse(bom));
    byte[] bad = {'"', (byte) 0xFF, 'a', (byte) 0xC3, '"'};
    assertEquals("\uFFFDa\uFFFD", parse(bad));
  }

  @Test
  void aLoneSurrogateEscapeIsKept() {
    assertEquals("\uD800", Json.parse("\"\\ud800\""));
    assertEquals("\"\\ud800\"", Json.stringify("\uD800"));
    assertEquals("\"\\ud83d\\ude00\"".length() > 0, true);
    assertEquals("\uD83D\uDE00", Json.parse("\"\\ud83d\\ude00\""));
  }

  @Test
  void nestingToFiveHundredAndTwelveIsRead() {
    String deep = "[".repeat(600) + "]".repeat(600);
    Object value = Json.parse(deep);
    int depth = 0;
    while (value instanceof List<?> list && !list.isEmpty()) {
      value = list.get(0);
      depth++;
    }
    assertEquals(599, depth);
  }

  @Test
  void malformedTextIsAJsonException() {
    for (String text : new String[] {"", "{", "[1,]", "{\"a\":}", "nul", "01", "\"\\x\"", "1 2"}) {
      assertThrows(JsonException.class, () -> Json.parse(text), text);
    }
  }

  @Test
  void stringifyWritesTheShapesTheUsageBodyNeeds() {
    Map<String, Object> entry = new LinkedHashMap<>();
    entry.put("flag", "f");
    entry.put("variationIndex", 1.0);
    entry.put("count", 3.0);
    assertEquals("{\"flag\":\"f\",\"variationIndex\":1,\"count\":3}", Json.stringify(entry));
    assertEquals("0", Json.stringify(-0.0));
    assertEquals("null", Json.stringify(Double.NaN));
    assertEquals("[1,null,true]", Json.stringify(java.util.Arrays.asList(1L, null, true)));
  }

  @Test
  void utf8RoundTripsAndReplacesLoneSurrogates() {
    byte[] bytes = Utf8.encode("a\uD83D\uDE00\u00e9");
    assertEquals("a\uD83D\uDE00\u00e9", Utf8.decode(bytes, 0, bytes.length));
    assertTrue(new String(Utf8.encode("\uD800"), StandardCharsets.UTF_8).equals("\uFFFD"));
  }

  @Test
  void anIncrementalDecoderJoinsASequenceSplitAcrossReads() {
    byte[] bytes = "\u00e9\uD83D\uDE00".getBytes(StandardCharsets.UTF_8);
    Utf8.Decoder decoder = new Utf8.Decoder();
    StringBuilder out = new StringBuilder();
    for (byte b : bytes) {
      decoder.write(new byte[] {b}, 0, 1, out);
    }
    decoder.finish(out);
    assertEquals("\u00e9\uD83D\uDE00", out.toString());
  }
}
