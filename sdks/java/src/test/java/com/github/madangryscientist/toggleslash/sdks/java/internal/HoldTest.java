package com.github.madangryscientist.toggleslash.sdks.java.internal;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.testkit.Fake;
import java.net.http.HttpHeaders;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** The Retry-After holds and the 300 second cap, on each of the three routes. */
@Timeout(60)
class HoldTest {
  private static HttpHeaders headers(String value) {
    return HttpHeaders.of(Map.of("Retry-After", List.of(value)), (a, b) -> true);
  }

  private static boolean until(long millis, BooleanSupplier condition) throws InterruptedException {
    long end = System.nanoTime() + millis * 1_000_000L;
    while (System.nanoTime() < end) {
      if (condition.getAsBoolean()) {
        return true;
      }
      Thread.sleep(5);
    }
    return condition.getAsBoolean();
  }

  @Test
  void retryAfterReadsWholeSecondsAndCapsAtThreeHundred() {
    assertEquals(7, Hub.retryAfter(headers("7")));
    assertEquals(7, Hub.retryAfter(headers("  7 ")));
    assertEquals(300, Hub.retryAfter(headers("300")));
    assertEquals(300, Hub.retryAfter(headers("301")));
    assertEquals(300, Hub.retryAfter(headers("99999999999999999999")));
    assertEquals(-1, Hub.retryAfter(headers("1.5")));
    assertEquals(-1, Hub.retryAfter(headers("-3")));
    assertEquals(-1, Hub.retryAfter(headers("soon")));
    assertEquals(-1, Hub.retryAfter(headers("")));
    assertEquals(-1, Hub.retryAfter(HttpHeaders.of(Map.of(), (a, b) -> true)));
  }

  @Test
  void aRulesetAnswerOf429HoldsTheRulesRoutesAtMostThreeHundredSeconds() throws Exception {
    Fake fake = new Fake();
    fake.ruleset =
        x -> {
          x.getResponseHeaders().set("Retry-After", "99999");
          Fake.reply(x, 429, "{}");
        };
    try (fake) {
      Hub hub = new Hub(Options.builder().url(fake.url()).sdkKey("k").stream(false).build(), null);
      Sync sync = new Sync(hub);
      sync.start();
      assertTrue(until(5000, () -> hub.rulesHoldMillis() > 0));
      long held = hub.rulesHoldMillis();
      assertTrue(held <= 300_000 && held > 290_000, "held " + held);
      assertEquals(0, hub.usageHoldMillis(), "the events route is held on its own");
      hub.beginClose();
    }
  }

  @Test
  void aStreamAnswerOf429HoldsTheRulesRoutesToo() throws Exception {
    Fake fake = new Fake();
    fake.ruleset = x -> Fake.rulesetReply(x, "\"v1\"", Fake.body());
    fake.stream =
        x -> {
          x.getResponseHeaders().set("Retry-After", "5000");
          Fake.reply(x, 429, "{}");
        };
    try (fake) {
      Hub hub = new Hub(Options.builder().url(fake.url()).sdkKey("k").build(), null);
      Sync sync = new Sync(hub);
      sync.start();
      assertTrue(until(5000, () -> hub.rulesHoldMillis() > 0));
      long held = hub.rulesHoldMillis();
      assertTrue(held <= 300_000 && held > 290_000, "held " + held);
      assertEquals(0, hub.usageHoldMillis());
      hub.beginClose();
    }
  }

  @Test
  void anEventsAnswerOf429HoldsOnlyTheEventsRoute() throws Exception {
    Fake fake = new Fake();
    fake.ruleset = x -> Fake.rulesetReply(x, "\"v1\"", Fake.body());
    fake.events =
        x -> {
          x.getResponseHeaders().set("Retry-After", "86400");
          Fake.reply(x, 429, "{}");
        };
    try (fake) {
      Hub hub =
          new Hub(
              Options.builder().url(fake.url()).sdkKey("k").stream(false)
                  .flushInterval(Duration.ofMinutes(5))
                  .build(),
              null);
      Usage usage = new Usage(hub);
      usage.count("f", 0);
      usage.flush(false);
      long held = hub.usageHoldMillis();
      assertTrue(held <= 300_000 && held > 290_000, "held " + held);
      assertEquals(0, hub.rulesHoldMillis());
      usage.count("f", 0);
      usage.flush(false);
      assertEquals(1, fake.count("/sdk/v1/events"), "a held route is not asked again");
      hub.beginClose();
    }
  }
}
