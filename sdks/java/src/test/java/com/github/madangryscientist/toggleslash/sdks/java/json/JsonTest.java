package com.github.madangryscientist.toggleslash.sdks.java.json;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import java.nio.charset.StandardCharsets;
import java.util.List;
import org.junit.jupiter.api.Test;

class JsonTest {
  @Test
  void parsesAndWritesRoundTrip() {
    String text = "{\"a\":[1,2.5,-0.25,true,false,null,\"x\\n\\u0001\"],\"b\":{}}";
    assertEquals(text, Json.parse(text).toJson());
  }

  @Test
  void refusesWhatIsNotJson() {
    for (String bad :
        List.of(
            "",
            "{",
            "[1,]",
            "{\"a\":1,}",
            "01",
            "1.",
            ".5",
            "+1",
            "\"\t\"",
            "nul",
            "[1] 2",
            "'a'",
            "{a:1}",
            "\"\\x\"")) {
      assertThrows(JsonException.class, () -> Json.parse(bad), bad);
    }
  }

  @Test
  void anOverflowingNumberIsInfinityAndWritesBackAsOne() {
    Json.Num n = (Json.Num) Json.parse("1e999");
    assertEquals(Double.POSITIVE_INFINITY, n.value());
    assertEquals("1e999", n.toJson());
    assertEquals(Double.NEGATIVE_INFINITY, ((Json.Num) Json.parse("-1e999")).value());
  }

  @Test
  void zeroAndNegativeZeroAreEqual() {
    assertEquals(Json.parse("0"), Json.parse("-0"));
  }

  @Test
  void deepNestingIsRefusedNotCrashed() {
    String deep = "[".repeat(5000) + "]".repeat(5000);
    assertThrows(JsonException.class, () -> Json.parse(deep));
    String fine = "[".repeat(600) + "]".repeat(600);
    assertEquals(fine, Json.parse(fine).toJson());
  }

  @Test
  void aBomIsSkippedAndBadBytesBecomeReplacements() {
    byte[] bom = {(byte) 0xEF, (byte) 0xBB, (byte) 0xBF, '[', '1', ']'};
    assertEquals("[1]", Json.parse(bom).toJson());
    byte[] bad = {'[', '"', (byte) 0xFF, '"', ']'};
    assertEquals("[\"\uFFFD\"]", Json.parse(bad).toJson());
  }

  @Test
  void aLoneSurrogateIsEscapedSoTheTextStaysValid() {
    Json.Str s = (Json.Str) Json.parse("\"\\ud800\"");
    assertEquals("\"\\ud800\"", s.toJson());
    assertEquals(s, Json.parse(s.toJson()));
  }

  @Test
  void duplicateNamesKeepTheLast() {
    assertEquals(new Json.Num(2), ((Json.Obj) Json.parse("{\"a\":1,\"a\":2}")).get("a"));
  }

  @Test
  void ofBuildsFromPlainObjects() {
    Json value = Json.of(java.util.Map.of("k", List.of(1, "x", true, 2.5)));
    assertEquals("{\"k\":[1,\"x\",true,2.5]}", value.toJson());
    assertThrows(IllegalArgumentException.class, () -> Json.of(new Object()));
  }

  @Test
  void utf8FollowsTheEncodingStandard() {
    // The standard's replacement counts: one U+FFFD per maximal bad subpart.
    assertEquals("\uFFFD", Utf8.decode(new byte[] {(byte) 0xE2, (byte) 0x82}));
    assertEquals("\uFFFD\uFFFD", Utf8.decode(new byte[] {(byte) 0xC0, (byte) 0x80}));
    assertEquals(
        "\uFFFD\uFFFD\uFFFD", Utf8.decode(new byte[] {(byte) 0xED, (byte) 0xA0, (byte) 0x80}));
    assertEquals("\uFFFDA", Utf8.decode(new byte[] {(byte) 0xE2, 'A'}));
    assertEquals("\uD83D\uDE00", Utf8.decode("\uD83D\uDE00".getBytes(StandardCharsets.UTF_8)));
    assertEquals("\uFEFFx", Utf8.decode(new byte[] {(byte) 0xEF, (byte) 0xBB, (byte) 0xBF, 'x'}));
  }

  @Test
  void utf8StreamingMatchesWholeInEveryChunking() {
    byte[] bytes = "a\u00e9\u20ac\uD83D\uDE00z".getBytes(StandardCharsets.UTF_8);
    for (int split = 0; split <= bytes.length; split++) {
      Utf8.Decoder decoder = new Utf8.Decoder(false);
      StringBuilder out = new StringBuilder();
      decoder.decode(bytes, 0, split, false, out);
      decoder.decode(bytes, split, bytes.length - split, true, out);
      assertEquals("a\u00e9\u20ac\uD83D\uDE00z", out.toString(), "split at " + split);
    }
  }

  @Test
  void utf8EncodesALoneSurrogateAsAReplacement() {
    assertEquals("\uFFFD", new String(Utf8.encode("\uD800"), StandardCharsets.UTF_8));
    assertEquals("\uD83D\uDE00", new String(Utf8.encode("\uD83D\uDE00"), StandardCharsets.UTF_8));
  }
}
