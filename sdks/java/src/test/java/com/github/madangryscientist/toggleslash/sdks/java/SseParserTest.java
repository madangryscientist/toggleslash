package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;

class SseParserTest {
  private static List<SseParser.Event> feedAll(String... pieces) {
    SseParser parser = new SseParser();
    List<SseParser.Event> out = new ArrayList<>();
    for (String piece : pieces) {
      out.addAll(parser.feed(piece));
    }
    return out;
  }

  @Test
  void anEventNeedsItsBlankLine() {
    assertTrue(feedAll("event: changed\ndata: \"v1\"\n").isEmpty());
    assertEquals(
        List.of(new SseParser.Event("changed", "\"v1\"")),
        feedAll("event: changed\ndata: \"v1\"\n\n"));
  }

  @Test
  void lineEndingsAreLfCrlfAndLoneCr() {
    assertEquals(
        List.of(
            new SseParser.Event("a", "1"),
            new SseParser.Event("b", "2"),
            new SseParser.Event("c", "3")),
        feedAll("event: a\ndata: 1\n\nevent: b\r\ndata: 2\r\n\r\nevent: c\rdata: 3\r\r"));
  }

  @Test
  void aCrlfSplitAcrossReadsIsOneLineEnd() {
    assertEquals(
        List.of(new SseParser.Event("a", "1")), feedAll("event: a\r\ndata: 1\r", "\n\r", "\n"));
  }

  @Test
  void anyChunkingGivesTheSameEvents() {
    String text =
        "event: connected\ndata: \"x\"\n\n: keep-alive\n\nevent: changed\ndata: \"y\"\n\n";
    List<SseParser.Event> whole = feedAll(text);
    String[] bytes = text.split("");
    assertEquals(whole, feedAll(bytes));
    assertEquals(2, whole.size());
  }

  @Test
  void commentsAndOtherFieldsAreDropped() {
    assertEquals(
        List.of(new SseParser.Event("message", "x")),
        feedAll(": hello\nid: 7\nretry: 5\ndata: x\n\n"));
  }

  @Test
  void severalDataLinesJoinWithLf() {
    assertEquals(
        List.of(new SseParser.Event("e", "a\nb\n")),
        feedAll("event: e\ndata: a\ndata: b\ndata:\n\n"));
  }

  @Test
  void anEventWithNoDataIsDroppedAndTheNameDoesNotLeak() {
    assertEquals(
        List.of(new SseParser.Event("message", "x")), feedAll("event: lost\n\ndata: x\n\n"));
  }

  @Test
  void onlyOneLeadingSpaceIsStripped() {
    assertEquals(List.of(new SseParser.Event("message", " x")), feedAll("data:  x\n\n"));
    assertEquals(List.of(new SseParser.Event("message", "x")), feedAll("data:x\n\n"));
  }

  @Test
  void aFieldWithNoColonIsTheWholeLine() {
    assertEquals(List.of(new SseParser.Event("message", "")), feedAll("data\n\n"));
  }

  @Test
  void anIncompleteEventAtTheEndIsNeverReturned() {
    assertTrue(feedAll("event: changed\ndata: \"v2\"").isEmpty());
  }
}
