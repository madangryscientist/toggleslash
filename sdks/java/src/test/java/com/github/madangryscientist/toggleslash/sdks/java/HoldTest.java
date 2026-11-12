package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;

import org.junit.jupiter.api.Test;

/** The {@code Retry-After} header: whole decimal digits only, capped. */
class HoldTest {
  private static final long CAP = 300_000;

  @Test
  void wholeDigitsAreSeconds() {
    assertEquals(0, Hold.parse("0", CAP));
    assertEquals(5_000, Hold.parse("5", CAP));
    assertEquals(5_000, Hold.parse("  5  ", CAP));
    assertEquals(CAP, Hold.parse("300", CAP));
    assertEquals(CAP, Hold.parse("301", CAP));
    assertEquals(CAP, Hold.parse("99999999999999999999", CAP));
  }

  @Test
  void everythingElseIsIgnored() {
    for (String value :
        new String[] {
          null,
          "",
          " ",
          "1.5",
          "-1",
          "+1",
          "1e2",
          "0x10",
          "5s",
          "Wed, 21 Oct 2026 07:28:00 GMT",
          "\u0663"
        }) {
      assertEquals(-1, Hold.parse(value, CAP), String.valueOf(value));
    }
  }
}
