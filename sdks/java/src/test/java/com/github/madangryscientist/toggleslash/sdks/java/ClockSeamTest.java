package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** What needs a clock the test moves: the 300 s cap, the UTC midnight, a failing evaluator. */
@Timeout(60)
final class ClockSeamTest {
  private static final Map<String, Object> CTX = Map.of("kind", "user", "key", "u1");
  private static final String RULESET =
      Support.ruleset(Support.boolFlag("checkout", true, 1), Support.boolFlag("legacy", false, 1));

  private FakeServer server;
  private FakeClock clock;
  private Client client;
  private final List<Throwable> errors = Collections.synchronizedList(new ArrayList<>());

  @BeforeEach
  void start() {
    server = new FakeServer();
    clock = new FakeClock(Instant.parse("2026-11-12T12:00:00Z").toEpochMilli());
  }

  @AfterEach
  void stop() {
    if (client != null) {
      client.close();
    }
    server.close();
  }

  private Options.Builder options(boolean stream) {
    return Support.options(server).stream(stream)
        .pollInterval(Duration.ofSeconds(1))
        .requestTimeout(Duration.ofSeconds(5))
        .clock(clock)
        .onError(errors::add);
  }

  @Test
  void aRetryAfterAbove300IsHeldFor300SecondsOnTheRulesetRoute() {
    server.ruleset = (e, r) -> FakeServer.reply(e, 429, "", "Retry-After", "99999");
    client = Client.create(options(true).build());
    Support.until("the sync loop waits", 5000, () -> !clock.sleepsOf("toggleslash-sync").isEmpty());
    assertEquals(300_000L, clock.sleepsOf("toggleslash-sync").get(0));
  }

  @Test
  void aRetryAfterAbove300IsHeldFor300SecondsOnTheRulesetRouteWhenPolling() {
    server.ruleset = (e, r) -> FakeServer.reply(e, 429, "", "Retry-After", "99999");
    client = Client.create(options(false).build());
    Support.until("the sync loop waits", 5000, () -> !clock.sleepsOf("toggleslash-sync").isEmpty());
    assertEquals(300_000L, clock.sleepsOf("toggleslash-sync").get(0));
  }

  @Test
  void aRetryAfterAbove300IsHeldFor300SecondsOnTheStreamRoute() {
    server.ruleset = FakeServer.serving("\"v1\"", RULESET);
    server.stream = (e, r) -> FakeServer.reply(e, 429, "", "Retry-After", "99999");
    client = Client.create(options(true).build());
    Support.until("the sync loop waits", 5000, () -> !clock.sleepsOf("toggleslash-sync").isEmpty());
    assertEquals(300_000L, clock.sleepsOf("toggleslash-sync").get(0));
  }

  @Test
  void aRetryAfterAbove300HoldsTheUsageReportsFor300SecondsAndNotLonger() {
    server.ruleset = FakeServer.serving("\"v1\"", RULESET);
    server.events = (e, r) -> FakeServer.reply(e, 429, "", "Retry-After", "99999");
    client = Client.create(options(false).build());
    assertTrue(client.ready());
    client.booleanVariation("checkout", CTX, false);
    client.flush();
    assertEquals(1, server.to("/sdk/v1/events").size());
    clock.setMono(299_999);
    client.flush();
    assertEquals(1, server.to("/sdk/v1/events").size(), "still held at 299.999 s");
    clock.setMono(300_000);
    client.flush();
    assertEquals(2, server.to("/sdk/v1/events").size(), "sent at 300 s");
  }

  @Test
  void countsEitherSideOfAUtcMidnightGoOutAsTwoDays() {
    server.ruleset = FakeServer.serving("\"v1\"", RULESET);
    client = Client.create(options(false).build());
    assertTrue(client.ready());
    clock.setWall(Instant.parse("2026-11-12T23:59:59.999Z").toEpochMilli());
    client.booleanVariation("checkout", CTX, false);
    clock.setWall(Instant.parse("2026-11-13T00:00:00.000Z").toEpochMilli());
    client.booleanVariation("checkout", CTX, false);
    client.flush();
    List<FakeServer.Req> sent = server.to("/sdk/v1/events");
    assertEquals(1, sent.size());
    Map<?, ?> body = (Map<?, ?>) Json.parse(sent.get(0).body());
    List<?> events = (List<?>) body.get("events");
    assertEquals(2, events.size());
    List<String> days = new ArrayList<>();
    for (Object event : events) {
      Map<?, ?> entry = (Map<?, ?>) event;
      days.add((String) entry.get("day"));
      assertEquals(1.0, ((Number) entry.get("count")).doubleValue());
    }
    assertEquals(List.of("2026-11-12", "2026-11-13"), days);
  }

  @Test
  void anEvaluationThatRaisesGivesTheFallbackEvaluationFailedAndOneOnErrorCall() {
    server.ruleset = FakeServer.serving("\"v1\"", RULESET);
    List<EvaluationEvent> hooked = Collections.synchronizedList(new ArrayList<>());
    client =
        Client.create(
            options(false)
                .afterEvaluation(hooked::add)
                .evaluator((ruleset, flag, context) -> {
                  throw new IllegalStateException("boom");
                })
                .build());
    assertTrue(client.ready());
    errors.clear();
    Detail<Boolean> detail = client.booleanVariationDetail("checkout", CTX, false);
    assertEquals(false, detail.value());
    assertNull(detail.variationIndex());
    assertEquals(Reason.error(ErrorCode.EVALUATION_FAILED), detail.reason());
    assertEquals(1, errors.size());
    assertEquals(1, hooked.size());
    client.flush();
    assertEquals(0, server.to("/sdk/v1/events").size(), "a failed evaluation counts nothing");
  }
}
