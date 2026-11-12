package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;

import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;

/** The server-sent event rules of the WHATWG parser, one at a time. */
class SseParserTest {
  private static List<SseParser.Event> parse(String... chunks) {
    SseParser parser = new SseParser();
    List<SseParser.Event> out = new ArrayList<>();
    for (String chunk : chunks) {
      parser.feed(chunk, out);
    }
    return out;
  }

  private static SseParser.Event event(String name, String data) {
    return new SseParser.Event(name, data);
  }

  @Test
  void lfCrlfAndLoneCrEndLines() {
    assertEquals(List.of(event("changed", "a")), parse("event: changed\ndata: a\n\n"));
    assertEquals(List.of(event("changed", "a")), parse("event: changed\r\ndata: a\r\n\r\n"));
    assertEquals(List.of(event("changed", "a")), parse("event: changed\rdata: a\r\r"));
  }

  @Test
  void chunkSplitsAreTolerated() {
    assertEquals(
        List.of(event("changed", "ab")), parse("ev", "ent: chan", "ged\nda", "ta: ab\n", "\n"));
    assertEquals(
        List.of(event("changed", "x")), parse("event: changed\r", "\ndata: x\r", "\n\r", "\n"));
  }

  @Test
  void defaultsJoinsAndDrops() {
    assertEquals(List.of(event("message", "a")), parse("data: a\n\n"));
    assertEquals(List.of(event("message", "a\nb")), parse("data: a\ndata: b\n\n"));
    assertEquals(List.of(), parse(": comment\n\n"));
    assertEquals(List.of(), parse("event: changed\n\n"));
    assertEquals(List.of(event("message", "x")), parse(": hi\ndata: x\n\n"));
  }

  @Test
  void aSingleLeadingSpaceIsStripped() {
    assertEquals(List.of(event("message", " a")), parse("data:  a\n\n"));
    assertEquals(List.of(event("message", "a")), parse("data:a\n\n"));
  }

  @Test
  void anIncompleteEventAtTheEndIsDiscarded() {
    assertEquals(List.of(), parse("event: changed\ndata: a\n"));
    assertEquals(List.of(event("message", "1")), parse("data: 1\n\ndata: 2\n"));
  }

  @Test
  void aByteOrderMarkAtTheStartIsSkipped() {
    assertEquals(List.of(event("message", "a")), parse("\uFEFFdata: a\n\n"));
  }

  @Test
  void aLongLineIsNotCapped() {
    String big = "x".repeat(2_000_000);
    assertEquals(List.of(event("message", big)), parse("data: " + big + "\n\n"));
  }
}
