package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Utf8;
import java.nio.charset.StandardCharsets;
import java.util.List;
import org.junit.jupiter.api.Test;

/** Unit tests of the small parts: the Retry-After reader, the SSE parser and the UTF-8 decoder. */
class UnitTest {
  @Test
  void retryAfterCountsOnlyWholeDecimalSeconds() {
    assertEquals(0, Hold.parseSeconds("0"));
    assertEquals(5, Hold.parseSeconds("5"));
    assertEquals(5, Hold.parseSeconds(" 5\t"));
    assertEquals(300, Hold.parseSeconds("301"));
    assertEquals(300, Hold.parseSeconds("99999999999999999999999"));
    assertEquals(-1, Hold.parseSeconds(null));
    assertEquals(-1, Hold.parseSeconds(""));
    assertEquals(-1, Hold.parseSeconds("1.5"));
    assertEquals(-1, Hold.parseSeconds("-1"));
    assertEquals(-1, Hold.parseSeconds("+1"));
    assertEquals(-1, Hold.parseSeconds("1e2"));
    assertEquals(-1, Hold.parseSeconds("Wed, 21 Oct 2015 07:28:00 GMT"));
  }

  private static String data(byte[] chunk, SseParser parser) {
    List<SseParser.Event> events = parser.feed(chunk);
    StringBuilder out = new StringBuilder();
    for (SseParser.Event e : events) {
      out.append(e.name()).append('=').append(e.data()).append(';');
    }
    return out.toString();
  }

  @Test
  void sseParserSplitsOnAnyLineEndingAcrossChunks() {
    SseParser parser = new SseParser();
    assertEquals("", data("event: chan".getBytes(StandardCharsets.UTF_8), parser));
    assertEquals("", data("ged\r".getBytes(StandardCharsets.UTF_8), parser));
    assertEquals(
        "changed=\"a\";", data("\ndata: \"a\"\n\n".getBytes(StandardCharsets.UTF_8), parser));
    // A bare "data" line adds an empty line to the data; a lone CR ends a line.
    assertEquals("message=x\n;", data("data:x\rdata\r\r".getBytes(StandardCharsets.UTF_8), parser));
  }

  @Test
  void sseParserJoinsDataWithLineFeedAndDropsEventsWithoutData() {
    SseParser parser = new SseParser();
    assertEquals(
        "message=a\nb;", data("data: a\ndata: b\n\n".getBytes(StandardCharsets.UTF_8), parser));
    assertEquals("", data("event: ping\n\n".getBytes(StandardCharsets.UTF_8), parser));
    assertEquals("", data(": a comment\n\n".getBytes(StandardCharsets.UTF_8), parser));
    // The name does not carry over to the next event.
    assertEquals("message=c;", data("data: c\n\n".getBytes(StandardCharsets.UTF_8), parser));
  }

  @Test
  void sseParserSkipsOneBomAtTheStartOnly() {
    SseParser parser = new SseParser();
    byte[] bom = {(byte) 0xEF, (byte) 0xBB, (byte) 0xBF};
    byte[] rest = "data: a\n\n".getBytes(StandardCharsets.UTF_8);
    byte[] all = new byte[bom.length + rest.length];
    System.arraycopy(bom, 0, all, 0, bom.length);
    System.arraycopy(rest, 0, all, bom.length, rest.length);
    assertEquals("message=a;", data(all, parser));
  }

  @Test
  void sseParserReadsAMultiByteCharacterSplitAcrossChunks() {
    SseParser parser = new SseParser();
    byte[] bytes = "data: é\n\n".getBytes(StandardCharsets.UTF_8);
    assertEquals("", data(java.util.Arrays.copyOfRange(bytes, 0, 7), parser));
    assertEquals("message=é;", data(java.util.Arrays.copyOfRange(bytes, 7, bytes.length), parser));
  }

  @Test
  void utf8FollowsTheEncodingStandard() {
    // A lone continuation byte, an overlong form, a surrogate, a truncated sequence and a value
    // past
    // U+10FFFF each give U+FFFD, the way the Encoding standard's decoder does.
    assertEquals("�", Utf8.decode(new byte[] {(byte) 0x80}));
    assertEquals("��", Utf8.decode(new byte[] {(byte) 0xC0, (byte) 0x80}));
    assertEquals("���", Utf8.decode(new byte[] {(byte) 0xED, (byte) 0xA0, (byte) 0x80}));
    assertEquals("�", Utf8.decode(new byte[] {(byte) 0xE2, (byte) 0x82}));
    assertEquals(
        "����", Utf8.decode(new byte[] {(byte) 0xF4, (byte) 0x90, (byte) 0x80, (byte) 0x80}));
    assertEquals("a�b", Utf8.decode(new byte[] {'a', (byte) 0xFF, 'b'}));
    assertEquals(
        "😀", Utf8.decode(new byte[] {(byte) 0xF0, (byte) 0x9F, (byte) 0x98, (byte) 0x80}));
  }

  @Test
  void utf8EncodesAndRoundTrips() {
    String text = "aé€😀";
    assertArrayEquals(text.getBytes(StandardCharsets.UTF_8), Utf8.encode(text));
    assertEquals(text, Utf8.decode(Utf8.encode(text)));
    assertTrue(Utf8.encode("").length == 0);
  }
}
