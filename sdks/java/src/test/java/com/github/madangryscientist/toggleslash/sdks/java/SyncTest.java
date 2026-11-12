package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/**
 * Ruleset sync, the stream, the watchdog, the ETag and flag version pairing, and the UTC day of a
 * count.
 */
@Timeout(90)
final class SyncTest {
  private static final Object USER = Context.of("u1");

  private static Options.Builder options(FakeServer server) {
    return Options.builder()
        .url(server.url())
        .sdkKey("fs_server_test")
        .requestTimeout(Duration.ofSeconds(5))
        .pollInterval(Duration.ofSeconds(5))
        .flushInterval(Duration.ofHours(1))
        .jitter(() -> 0.0);
  }

  /** A clock a test moves by hand. */
  private static final class MovingClock extends Clock {
    private final AtomicReference<Instant> now;

    MovingClock(Instant start) {
      this.now = new AtomicReference<>(start);
    }

    void set(Instant instant) {
      now.set(instant);
    }

    @Override
    public ZoneOffset getZone() {
      return ZoneOffset.UTC;
    }

    @Override
    public Clock withZone(java.time.ZoneId zone) {
      return this;
    }

    @Override
    public Instant instant() {
      return now.get();
    }
  }

  @Test
  void aSecondFetchSendsIfNoneMatchAndA304KeepsTheRuleset() throws Exception {
    try (FakeServer server = new FakeServer();
        Client client =
            Client.create(
                options(server).stream(false).pollInterval(Duration.ofMillis(200)).build())) {
      assertTrue(client.ready());
      assertTrue(Eventually.holds(10_000, () -> server.to("/sdk/v1/ruleset").size() >= 3));
      List<FakeServer.Request> fetches = server.to("/sdk/v1/ruleset");
      assertFalse(fetches.get(0).headers().containsKey("if-none-match"));
      assertEquals("\"v1\"", fetches.get(1).headers().get("if-none-match"));
      assertTrue(client.boolVariation("checkout", USER, false));
      assertEquals("\"v1\"", client.status().etag());
      assertEquals(StreamState.OFF, client.status().stream());
    }
  }

  @Test
  void aChangedEventFetchesTheNewRulesetAndAConnectedEventWithTheSameTagDoesNot() throws Exception {
    try (FakeServer server = new FakeServer();
        Client client = Client.create(options(server).build())) {
      assertTrue(client.ready());
      assertTrue(Eventually.holds(5000, () -> client.status().stream() == StreamState.CONNECTED));
      assertEquals(1, server.to("/sdk/v1/ruleset").size());
      server.serve(FakeServer.rulesetJson(2, "checkout", "extra"), "\"v2\"");
      server.push("event: changed\ndata: \"v2\"\n\n");
      assertTrue(Eventually.holds(5000, () -> "\"v2\"".equals(client.status().etag())));
      assertTrue(client.boolVariation("extra", USER, false));
    }
  }

  @Test
  void aBadRulesetKeepsTheLastGoodOneAndSaysBadRuleset() throws Exception {
    try (FakeServer server = new FakeServer();
        Client client = Client.create(options(server).build())) {
      assertTrue(client.ready());
      assertTrue(Eventually.holds(5000, () -> client.status().stream() == StreamState.CONNECTED));
      server.serve("{\"flags\":{},\"segments\":[]}", "\"v2\"");
      server.push("event: changed\ndata: \"v2\"\n\n");
      assertTrue(
          Eventually.holds(5000, () -> Status.BAD_RULESET.equals(client.status().lastError())));
      assertEquals("\"v1\"", client.status().etag());
      assertTrue(client.boolVariation("checkout", USER, false));
    }
  }

  @Test
  void aStreamThatGoesQuietIsDroppedAndOpenedAgain() {
    try (FakeServer server = new FakeServer();
        Client client =
            Client.create(options(server).streamIdleTimeout(Duration.ofMillis(400)).build())) {
      assertTrue(client.ready());
      assertTrue(Eventually.holds(10_000, () -> server.to("/sdk/v1/stream").size() >= 2));
      assertTrue(
          Eventually.holds(
              5000,
              () -> {
                Status status = client.status();
                return status.stream() == StreamState.CONNECTED
                    || Status.TIMEOUT.equals(status.lastError());
              }));
    }
  }

  @Test
  void aKeepAliveRestartsTheWatchdog() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.stream =
          (exchange, request) -> {
            exchange.getResponseHeaders().set("Content-Type", "text/event-stream");
            exchange.sendResponseHeaders(200, 0);
            var out = exchange.getResponseBody();
            out.write(
                "event: connected\ndata: \"v1\"\n\n"
                    .getBytes(java.nio.charset.StandardCharsets.UTF_8));
            out.flush();
            for (int i = 0; i < 12; i++) {
              Thread.sleep(150);
              out.write(": keep-alive\n\n".getBytes(java.nio.charset.StandardCharsets.UTF_8));
              out.flush();
            }
            server.hang();
          };
      try (Client client =
          Client.create(options(server).streamIdleTimeout(Duration.ofMillis(500)).build())) {
        assertTrue(client.ready());
        Thread.sleep(1700);
        assertEquals(1, server.to("/sdk/v1/stream").size());
        assertEquals(StreamState.CONNECTED, client.status().stream());
        assertTrue(Eventually.holds(10_000, () -> server.to("/sdk/v1/stream").size() >= 2));
      }
    }
  }

  @Test
  void theEtagAndTheFlagVersionOfAnEventAlwaysBelongTogether() throws Exception {
    try (FakeServer server = new FakeServer()) {
      List<EvaluationEvent> events = new CopyOnWriteArrayList<>();
      try (Client client = Client.create(options(server).afterEvaluation(events::add).build())) {
        assertTrue(client.ready());
        assertTrue(Eventually.holds(5000, () -> client.status().stream() == StreamState.CONNECTED));
        client.boolVariation("checkout", USER, false);
        for (int version = 2; version <= 40; version++) {
          String tag = "\"v" + version + "\"";
          server.serve(FakeServer.rulesetJson(version, "checkout"), tag);
          server.push("event: changed\ndata: " + tag + "\n\n");
          long deadline = System.nanoTime() + Duration.ofSeconds(10).toNanos();
          while (!tag.equals(client.status().etag()) && System.nanoTime() < deadline) {
            client.boolVariation("checkout", USER, false);
          }
          assertEquals(tag, client.status().etag());
          client.boolVariation("checkout", USER, false);
        }
      }
      assertTrue(events.size() >= 39);
      long distinct = events.stream().map(EvaluationEvent::etag).distinct().count();
      assertEquals(40, distinct);
      for (EvaluationEvent event : events) {
        assertEquals("\"v" + event.flagVersion() + "\"", event.etag());
      }
    }
  }

  @Test
  void aCountIsSplitByTheUtcDayOfTheEvaluation() throws Exception {
    try (FakeServer server = new FakeServer()) {
      MovingClock clock = new MovingClock(Instant.parse("2026-03-01T23:59:59.900Z"));
      Client client = Client.create(options(server).clock(clock).build());
      try {
        assertTrue(client.ready());
        client.boolVariation("checkout", USER, false);
        client.boolVariation("checkout", USER, false);
        clock.set(Instant.parse("2026-03-02T00:00:00.100Z"));
        client.boolVariation("checkout", USER, false);
        assertEquals(2, client.pendingUsage());
        client.flush();
      } finally {
        client.close();
      }
      String body = server.to("/sdk/v1/events").get(0).body();
      assertTrue(body.contains("\"day\":\"2026-03-01\""), body);
      assertTrue(body.contains("\"day\":\"2026-03-02\""), body);
      assertTrue(body.contains("\"count\":2"), body);
      assertTrue(body.contains("\"count\":1"), body);
    }
  }

  @Test
  void aFiveHundredFromEventsKeepsTheCountsAndTheNextFlushSendsThemAgain() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.events = (exchange, request) -> FakeServer.send(exchange, 503, Map.of(), "down");
      Client client = Client.create(options(server).build());
      try {
        assertTrue(client.ready());
        client.boolVariation("checkout", USER, false);
        client.flush();
        assertEquals(1, client.pendingUsage());
        server.events = (exchange, request) -> FakeServer.send(exchange, 200, Map.of(), "{}");
        client.flush();
        assertEquals(0, client.pendingUsage());
      } finally {
        client.close();
      }
    }
  }

  @Test
  void aFourHundredFromEventsDropsTheBatch() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.events = (exchange, request) -> FakeServer.send(exchange, 400, Map.of(), "bad");
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Client client = Client.create(options(server).onError(errors::add).build());
      try {
        assertTrue(client.ready());
        client.boolVariation("checkout", USER, false);
        client.flush();
        assertEquals(0, client.pendingUsage());
        assertTrue(
            errors.stream()
                .anyMatch(e -> e instanceof ToggleSlashException t && "http_400".equals(t.code())));
      } finally {
        client.close();
      }
    }
  }

  @Test
  void closeSendsTheFinalFlush() {
    try (FakeServer server = new FakeServer()) {
      Client client = Client.create(options(server).build());
      assertTrue(client.ready());
      client.boolVariation("checkout", USER, false);
      client.close();
      assertEquals(1, server.to("/sdk/v1/events").size());
    }
  }

  @Test
  void everyRouteSendsTheHeadersTheProtocolNames() throws Exception {
    try (FakeServer server = new FakeServer()) {
      Client client =
          Client.create(options(server).pollInterval(Duration.ofMillis(200)).stream(false).build());
      try {
        assertTrue(client.ready());
        assertTrue(Eventually.holds(10_000, () -> server.to("/sdk/v1/ruleset").size() >= 2));
        client.boolVariation("checkout", USER, false);
        client.flush();
      } finally {
        client.close();
      }
      try (Client streaming = Client.create(options(server).build())) {
        assertTrue(streaming.ready());
        assertTrue(Eventually.holds(5000, () -> !server.to("/sdk/v1/stream").isEmpty()));
      }
      FakeServer.Request first = server.to("/sdk/v1/ruleset").get(0);
      FakeServer.Request second = server.to("/sdk/v1/ruleset").get(1);
      FakeServer.Request stream = server.to("/sdk/v1/stream").get(0);
      FakeServer.Request events = server.to("/sdk/v1/events").get(0);
      for (FakeServer.Request request : List.of(first, second, stream, events)) {
        assertEquals("Bearer fs_server_test", request.headers().get("authorization"));
      }
      assertEquals("GET", first.method());
      assertFalse(first.headers().containsKey("if-none-match"));
      assertEquals("\"v1\"", second.headers().get("if-none-match"));
      assertEquals("text/event-stream", stream.headers().get("accept"));
      assertEquals("POST", events.method());
      assertEquals("application/json", events.headers().get("content-type"));
      for (FakeServer.Request request : List.of(first, second, stream, events)) {
        System.out.println(
            request.method() + " " + request.path() + " " + request.headers().keySet());
      }
    }
  }
}
