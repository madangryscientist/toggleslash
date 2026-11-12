package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

/** The leaf modules on their own: the codec, the JSON reader and writer, SSE and the parsers. */
class LeafTest {
  private static byte[] bytes(int... values) {
    byte[] out = new byte[values.length];
    for (int i = 0; i < values.length; i++) {
      out[i] = (byte) values[i];
    }
    return out;
  }

  @Test
  void utf8DecodesEachMaximalInvalidSubpartToOneReplacement() {
    // The Encoding standard's own examples for the decoder.
    assertEquals("����", Utf8.decode(bytes(0xF0, 0x80, 0x80, 0x80)));
    assertEquals("���", Utf8.decode(bytes(0xED, 0xA0, 0x80)));
    assertEquals("�", Utf8.decode(bytes(0xE2, 0x82)));
    assertEquals("�A", Utf8.decode(bytes(0xE2, 0x82, 0x41)));
    assertEquals("��", Utf8.decode(bytes(0xC0, 0xAF)));
    assertEquals("�", Utf8.decode(bytes(0xF0, 0x90, 0x80)));
    assertEquals("��", Utf8.decode(bytes(0xFF, 0xFE)));
    assertEquals("€", Utf8.decode(bytes(0xE2, 0x82, 0xAC)));
    assertEquals("😀", Utf8.decode(bytes(0xF0, 0x9F, 0x98, 0x80)));
    assertEquals("﻿a", Utf8.decode(bytes(0xEF, 0xBB, 0xBF, 'a')), "the BOM is kept");
    assertEquals("", Utf8.decode(new byte[0]));
  }

  @Test
  void utf8EncodesALoneSurrogateAsReplacementNotQuestionMark() {
    assertArrayEquals(bytes(0xEF, 0xBF, 0xBD), Utf8.encode("\ud800"));
    assertArrayEquals(bytes(0xEF, 0xBF, 0xBD, 'a'), Utf8.encode("\udc00a"));
    assertArrayEquals(bytes(0xF0, 0x9F, 0x98, 0x80), Utf8.encode("😀"));
    assertArrayEquals("hé".getBytes(StandardCharsets.UTF_8), Utf8.encode("hé"));
  }

  @Test
  void jsonReadsTheModelAndRejectsWhatIsNotJson() {
    assertEquals(Map.of("a", List.of(1.0, true, "x")), Json.parse(" {\"a\": [1, true, \"x\"]} "));
    assertEquals(1.5e3, Json.parse("1.5e3"));
    assertNull(Json.parse("null"));
    assertEquals("😀", Json.parse("\"\\ud83d\\ude00\""));
    for (String bad :
        new String[] {
          "", "{", "[1,]", "{\"a\":1,}", "01", "1.", "+1", "'a'", "[1] x", "\"\t\"", "nul", "{1:2}"
        }) {
      assertThrows(IllegalArgumentException.class, () -> Json.parse(bad), bad);
    }
    assertThrows(IllegalArgumentException.class, () -> Json.parse("[".repeat(100_000)));
  }

  @Test
  void jsonWritesWhatItReads() {
    assertEquals(
        "{\"a\":[1,2.5,null,true,\"x\\n\"]}",
        Json.write(
            new java.util.LinkedHashMap<>(
                Map.of("a", java.util.Arrays.asList(1.0, 2.5, null, true, "x\n")))));
    assertEquals("9223372036854775807", Json.write(Long.MAX_VALUE));
    assertEquals("null", Json.write(Double.NaN));
    assertEquals("null", Json.write(Double.POSITIVE_INFINITY));
    Object round = Json.parse(Json.write(Map.of("k", "é😀\u0001")));
    assertEquals(Map.of("k", "é😀\u0001"), round);
  }

  private static List<String> sse(byte[]... chunks) {
    List<String> out = new ArrayList<>();
    Sse parser = new Sse(e -> out.add(e.name() + "=" + e.data()));
    for (byte[] chunk : chunks) {
      parser.feed(chunk, chunk.length);
    }
    return out;
  }

  @Test
  void sseSplitsAtAnyByteAndStillReadsTheSameEvents() {
    byte[] all =
        ("﻿event: connected\ndata: \"v1\"\n\n: keep-alive\n\nevent: changed\r\ndata: a\rdata: b\r\n\r\ndata: x\n\n")
            .getBytes(StandardCharsets.UTF_8);
    List<String> whole = sse(all);
    assertEquals(List.of("connected=\"v1\"", "changed=a\nb", "message=x"), whole);
    for (int cut = 0; cut <= all.length; cut++) {
      byte[] head = java.util.Arrays.copyOfRange(all, 0, cut);
      byte[] tail = java.util.Arrays.copyOfRange(all, cut, all.length);
      assertEquals(whole, sse(head, tail), "cut at " + cut);
    }
    // One byte at a time.
    byte[][] single = new byte[all.length][];
    for (int i = 0; i < all.length; i++) {
      single[i] = new byte[] {all[i]};
    }
    assertEquals(whole, sse(single));
  }

  @Test
  void sseIgnoresAnEventWithNoData() {
    assertEquals(List.of(), sse("event: changed\n\n".getBytes(StandardCharsets.UTF_8)));
    assertEquals(List.of("message="), sse("data:\n\n".getBytes(StandardCharsets.UTF_8)));
    assertEquals(List.of("message=a"), sse("data:a\n\n".getBytes(StandardCharsets.UTF_8)));
    assertEquals(List.of("message= a"), sse("data:  a\n\n".getBytes(StandardCharsets.UTF_8)));
  }

  @Test
  void attributeReferencesSplitOnSlashAndDecodeTilde() {
    assertEquals(List.of("a", "b"), AttributeReference.parse("/a/b"));
    assertEquals(List.of("a/b", "c~d"), AttributeReference.parse("/a~1b/c~0d"));
    assertEquals(List.of("~1"), AttributeReference.parse("/~01"));
    assertNull(AttributeReference.parse("/a//b"));
    assertNull(AttributeReference.parse("/"));
    assertNull(AttributeReference.parse("/a~2"));
    assertNull(AttributeReference.parse("/a~"));
  }

  @Test
  void rfcInstantsAcceptOnlyTheStrictForm() {
    assertEquals(1_700_000_000_000.0, RfcInstant.parse("2023-11-14T22:13:20Z"));
    assertEquals(1_700_000_000_500.0, RfcInstant.parse("2023-11-14T22:13:20.5Z"));
    assertEquals(1_700_000_000_000.0, RfcInstant.parse("2023-11-15T00:13:20+02:00"));
    assertNotNull(RfcInstant.parse("2024-02-29T00:00:00Z"));
    for (String bad :
        new String[] {
          "2023-02-29T00:00:00Z",
          "2023-13-01T00:00:00Z",
          "2023-11-14 22:13:20Z",
          "2023-11-14T22:13:20",
          "2023-11-14T24:00:00Z",
          "2023-11-14T22:13:20.Z",
          "nope",
          "",
          "2023-11-14t22:13:20z"
        }) {
      assertNull(RfcInstant.parse(bad), bad);
    }
  }

  @Test
  void semanticVersionsOrderByPrecedence() {
    assertTrue(SemanticVersion.parse("1.0.0-alpha").compareTo(SemanticVersion.parse("1.0.0")) < 0);
    assertTrue(
        SemanticVersion.parse("1.0.0-alpha.1").compareTo(SemanticVersion.parse("1.0.0-alpha.beta"))
            < 0);
    assertTrue(SemanticVersion.parse("1.0.0-2").compareTo(SemanticVersion.parse("1.0.0-10")) < 0);
    assertEquals(0, SemanticVersion.parse("1.2.3+a").compareTo(SemanticVersion.parse("1.2.3+b")));
    assertTrue(SemanticVersion.parse("1.10.0").compareTo(SemanticVersion.parse("1.9.0")) > 0);
    assertEquals(0, SemanticVersion.parse("2.1").compareTo(SemanticVersion.parse("2.1.0")));
    assertEquals(0, SemanticVersion.parse("2").compareTo(SemanticVersion.parse("2.0.0")));
    assertTrue(
        SemanticVersion.parse("1.0.99999999999999999999")
                .compareTo(SemanticVersion.parse("1.0.99999999999999999998"))
            > 0);
    for (String bad :
        new String[] {
          "01.2.3",
          "1.2.3-",
          "1.2.3-01",
          "a.b.c",
          "",
          "1.2.3+",
          "1.2.3.4",
          "v1.2.3",
          " 1.2.3",
          "1.2-beta"
        }) {
      assertNull(SemanticVersion.parse(bad), bad);
    }
  }

  @Test
  void patternsFollowTheSpecifiedSubset() {
    assertTrue(Pattern.compile("^a+b$").isMatch("aaab"));
    assertTrue(!Pattern.compile("^a+b$").isMatch("aaa"));
    assertTrue(Pattern.compile("a|b").isMatch("xxb"));
    assertTrue(Pattern.compile("\\d{2,3}").isMatch("a123"));
    assertTrue(Pattern.compile("").isMatch("anything"));
    assertTrue(Pattern.compile("\\D").isMatch("\u0661"));
    assertTrue(!Pattern.compile("\\d").isMatch("\u0661"));
    assertTrue(Pattern.compile("^.$").isMatch("\ud83d\ude00"));
    assertTrue(!Pattern.compile("a$").isMatch("a\n"));
    assertTrue(Pattern.compile("[-a]").isMatch("-"));
    assertNull(Pattern.compile("(?i)abc"));
    assertNull(Pattern.compile("[a-z-9]"));
    assertNull(Pattern.compile("a+?"));
    assertNull(Pattern.compile("{,2}"));
    assertNull(Pattern.compile("("));
    assertNull(Pattern.compile("a{2,1}"));
    assertNull(Pattern.compile("*a"));
    assertNull(Pattern.compile("(a)\\1"));
    assertNull(Pattern.compile("(?=a)"));
    assertNull(Pattern.compile("]"));
  }
}
