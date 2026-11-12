package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;

import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;

/** The small parsers: {@code Retry-After} and server-sent events. */
final class ParsingTest {
  @Test
  void retryAfterCountsOnlyWholeDecimalSecondsAndCapsAt300() {
    assertEquals(5, RetryAfter.seconds("5"));
    assertEquals(7, RetryAfter.seconds("  7 "));
    assertEquals(0, RetryAfter.seconds("0"));
    assertEquals(300, RetryAfter.seconds("300"));
    assertEquals(300, RetryAfter.seconds("301"));
    assertEquals(300, RetryAfter.seconds("99999999999999999999999"));
    assertEquals(-1, RetryAfter.seconds(null));
    assertEquals(-1, RetryAfter.seconds(""));
    assertEquals(-1, RetryAfter.seconds("  "));
    assertEquals(-1, RetryAfter.seconds("1.5"));
    assertEquals(-1, RetryAfter.seconds("-1"));
    assertEquals(-1, RetryAfter.seconds("+1"));
    assertEquals(-1, RetryAfter.seconds("Wed, 21 Oct 2026 07:28:00 GMT"));
  }

  private static List<String> parse(String... chunks) {
    List<String> seen = new ArrayList<>();
    SseParser parser =
        new SseParser((name, data) -> {
          seen.add(name + "=" + data);
          return true;
        });
    for (String chunk : chunks) {
      parser.accept(chunk);
    }
    return seen;
  }

  @Test
  void anEventIsReadAcrossChunksAndLineEnds() {
    assertEquals(
        List.of("changed=\"v2\""), parse("event: chan", "ged\r\ndata: \"v2\"\r", "\n\r\n"));
    assertEquals(List.of("connected=a\nb"), parse("event: connected\ndata: a\ndata: b\n\n"));
    assertEquals(List.of("connected=x"), parse("event: connected\rdata: x\r\r"));
  }

  @Test
  void commentsAndIncompleteEventsAreNotEvents() {
    assertEquals(List.of(), parse(": keep-alive\n\n"));
    assertEquals(List.of(), parse("event: changed\ndata: x\n"));
    assertEquals(List.of("changed=x"), parse("﻿event: changed\ndata: x\n\n"));
  }
}
