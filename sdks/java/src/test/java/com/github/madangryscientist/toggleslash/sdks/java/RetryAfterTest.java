package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;

import java.net.http.HttpHeaders;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class RetryAfterTest {
  private static HttpHeaders headers(String value) {
    return HttpHeaders.of(Map.of("Retry-After", List.of(value)), (k, v) -> true);
  }

  @Test
  void wholeSecondsOnly() {
    assertEquals(7_000, RetryAfter.millis(429, headers("7")));
    assertEquals(7_000, RetryAfter.millis(429, headers(" 7 ")));
    assertEquals(0, RetryAfter.millis(429, headers("0")));
    assertEquals(-1, RetryAfter.millis(429, headers("1.5")));
    assertEquals(-1, RetryAfter.millis(429, headers("-3")));
    assertEquals(-1, RetryAfter.millis(429, headers("Wed, 21 Oct 2026 07:28:00 GMT")));
    assertEquals(-1, RetryAfter.millis(429, headers("")));
    assertEquals(-1, RetryAfter.millis(429, HttpHeaders.of(Map.of(), (k, v) -> true)));
  }

  @Test
  void onlyA429IsRead() {
    assertEquals(-1, RetryAfter.millis(503, headers("7")));
    assertEquals(-1, RetryAfter.millis(200, headers("7")));
  }

  @Test
  void capsAtFiveMinutes() {
    assertEquals(300_000, RetryAfter.millis(429, headers("301")));
    assertEquals(300_000, RetryAfter.millis(429, headers("99999999999999999999")));
    assertEquals(299_000, RetryAfter.millis(429, headers("299")));
  }
}
