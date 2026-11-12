package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;

import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;

class SseParserTest {
  private static final String STREAM =
      ": hello\r\nevent: connected\r\ndata: \"v1\"\r\n\r\n"
          + "data: a\ndata: b\n\n"
          + "event: changed\ndata:\"v2\"\n\n"
          + "event: nodata\n\n"
          + "id: 3\nretry: 5\nevent: changed\ndata: x\n\r\r";

  private static List<SseParser.Event> parse(String... chunks) {
    SseParser parser = new SseParser();
    List<SseParser.Event> all = new ArrayList<>();
    for (String chunk : chunks) {
      all.addAll(parser.feed(chunk));
    }
    return all;
  }

  @Test
  void readsEventsDataLinesAndDefaultNames() {
    List<SseParser.Event> events = parse(STREAM);
    assertEquals(
        List.of(
            new SseParser.Event("connected", "\"v1\""),
            new SseParser.Event("message", "a\nb"),
            new SseParser.Event("changed", "\"v2\""),
            new SseParser.Event("changed", "x")),
        events);
  }

  @Test
  void everySplitPointGivesTheSameEvents() {
    List<SseParser.Event> whole = parse(STREAM);
    for (int i = 0; i <= STREAM.length(); i++) {
      assertEquals(whole, parse(STREAM.substring(0, i), STREAM.substring(i)), "split at " + i);
    }
  }

  @Test
  void anUnfinishedEventIsDropped() {
    assertEquals(List.of(), parse("event: changed\ndata: x\n"));
  }
}
