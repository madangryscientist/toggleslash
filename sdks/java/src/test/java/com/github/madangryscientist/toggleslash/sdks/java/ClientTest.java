package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeSet;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;

/** The client against a loopback server: results, counting, holds, hooks and the wire. */
class ClientTest {
  private static final Map<String, Object> USER = Map.of("key", "u1");

  private static ToggleSlashClient poll(FakeServer server, Options.Builder extra) {
    return ToggleSlashClient.create(extra.build());
  }

  private static Options.Builder options(FakeServer server) {
    return Options.builder(server.url(), "fs_server_test").stream(false)
        .pollInterval(Duration.ofMillis(60_000))
        .flushInterval(Duration.ofMillis(60_000));
  }

  private static void serve(FakeServer server, String body) {
    server.on(
        "/sdk/v1/ruleset",
        x -> FakeServer.reply(x, 200, body, "ETag", "\"v1\"", "Content-Type", "application/json"));
    server.on("/sdk/v1/events", x -> FakeServer.reply(x, 200, "{}"));
  }

  @Test
  void accessorsApplyTheMismatchRuleAndTheInt64Range() throws Exception {
    try (FakeServer server = new FakeServer()) {
      serve(
          server,
          FakeServer.ruleset(
              FakeServer.flag("b", 1, "false", "true"),
              FakeServer.flag("s", 1, "\"a\"", "\"b\""),
              FakeServer.flag("n", 1, "1.5"),
              FakeServer.flag("two", 1, "2.0"),
              FakeServer.flag("frac", 1, "2.5"),
              FakeServer.flag("huge", 1, "1e19"),
              FakeServer.flag("edge", 1, "9223372036854774784"),
              FakeServer.flag("low", 1, "-9223372036854775808"),
              FakeServer.flag("obj", 1, "{\"a\":[1]}"),
              FakeServer.flag("arr", 1, "[1,2]"),
              FakeServer.flag("nul", 1, "null")));
      try (ToggleSlashClient client = poll(server, options(server))) {
        assertTrue(client.ready());
        assertTrue(client.boolVariation("b", USER, false));
        assertEquals("b", client.stringVariation("s", USER, "x"));
        assertEquals(1.5, client.doubleVariation("n", USER, 0));
        assertEquals(2L, client.intVariation("two", USER, 0));
        assertEquals(9223372036854774784L, client.intVariation("edge", USER, 0));
        assertEquals(Long.MIN_VALUE, client.intVariation("low", USER, 0));

        Detail<Long> frac = client.intVariationDetail("frac", USER, 7);
        assertEquals(7L, frac.value());
        assertNull(frac.variationIndex());
        assertEquals(Reason.error(ErrorCode.TYPE_MISMATCH), frac.reason());
        assertEquals(
            Reason.error(ErrorCode.TYPE_MISMATCH),
            client.intVariationDetail("huge", USER, 7).reason());
        assertEquals(1.5, client.doubleVariation("n", USER, 0));
        assertEquals(
            Reason.error(ErrorCode.TYPE_MISMATCH),
            client.boolVariationDetail("s", USER, true).reason());

        assertEquals(Map.of("a", List.of(1.0)), client.variation("obj", USER, Map.of()));
        assertEquals(List.of(1.0, 2.0), client.variation("arr", USER, List.of()));
        assertEquals(
            Reason.error(ErrorCode.TYPE_MISMATCH),
            client.variationDetail("obj", USER, List.of()).reason());
        assertEquals(
            Reason.error(ErrorCode.TYPE_MISMATCH),
            client.variationDetail("nul", USER, "x").reason());
        assertNull(client.variation("nul", USER, null));
        assertEquals(Detail.class, client.variationDetail("nul", USER, null).getClass());
        assertEquals(0, client.variationDetail("nul", USER, null).variationIndex());

        Detail<Boolean> missing = client.boolVariationDetail("nope", USER, true);
        assertTrue(missing.value());
        assertEquals(Reason.error(ErrorCode.FLAG_NOT_FOUND), missing.reason());
        assertEquals(
            Reason.error(ErrorCode.INVALID_CONTEXT),
            client.boolVariationDetail("b", Map.of("kind", "user"), false).reason());
        assertEquals(
            Reason.error(ErrorCode.INVALID_CONTEXT),
            client.boolVariationDetail("b", new Object(), false).reason());
      }
    }
  }

  @Test
  void evaluationBeforeTheFirstLoadIsNotReady() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.on("/sdk/v1/ruleset", x -> FakeServer.hang());
      try (ToggleSlashClient client =
          poll(server, options(server).requestTimeout(Duration.ofSeconds(30)))) {
        Detail<Boolean> detail = client.boolVariationDetail("b", USER, true);
        assertTrue(detail.value());
        assertEquals(Reason.error(ErrorCode.NOT_READY), detail.reason());
        assertNull(client.status().etag());
      }
    }
  }

  @Test
  void anEvaluationThatRaisesGivesTheFallbackAndOneOnError() throws Exception {
    try (FakeServer server = new FakeServer()) {
      serve(server, FakeServer.ruleset(FakeServer.flag("b", 1, "false", "true")));
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Options.Builder builder =
          options(server)
              .onError(errors::add)
              .evaluator((ruleset, key, ctx) -> {
                throw new IllegalStateException("evaluator broke");
              });
      try (ToggleSlashClient client = poll(server, builder)) {
        assertTrue(client.ready());
        Detail<Boolean> detail = client.boolVariationDetail("b", USER, false);
        assertFalse(detail.value());
        assertNull(detail.variationIndex());
        assertEquals(Reason.error(ErrorCode.EVALUATION_FAILED), detail.reason());
        assertEquals(1, errors.size());
        assertEquals("evaluator broke", errors.get(0).getMessage());
        client.flushNow();
        assertTrue(server.to("/sdk/v1/events").isEmpty());
      }
    }
  }

  @Test
  void aHookThatThrowsReachesOnErrorAndNotTheCaller() throws Exception {
    try (FakeServer server = new FakeServer()) {
      serve(server, FakeServer.ruleset(FakeServer.flag("b", 1, "false", "true")));
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Options.Builder builder =
          options(server)
              .onError(e -> {
                errors.add(e);
                throw new IllegalStateException("onError threw too");
              })
              .afterEvaluation(e -> {
                throw new IllegalStateException("hook threw");
              });
      try (ToggleSlashClient client = poll(server, builder)) {
        assertTrue(client.ready());
        assertTrue(client.boolVariation("b", USER, false));
        assertEquals(1, errors.size());
        client.flushNow();
        assertEquals(1, server.to("/sdk/v1/events").size());
      }
    }
  }

  @Test
  void hookEventsCarryTheEtagAndTheFlagVersionOfTheRulesetUsed() throws Exception {
    try (FakeServer server = new FakeServer()) {
      serve(server, FakeServer.ruleset(FakeServer.flag("b", 7, "false", "true")));
      List<EvaluationEvent> events = new CopyOnWriteArrayList<>();
      try (ToggleSlashClient client = poll(server, options(server).afterEvaluation(events::add))) {
        assertTrue(client.ready());
        client.boolVariation("b", USER, false);
        client.boolVariation("missing", USER, false);
        assertEquals(2, events.size());
        assertEquals("\"v1\"", events.get(0).etag());
        assertEquals(7L, events.get(0).flagVersion());
        assertSame(USER, events.get(0).context());
        assertEquals(1, events.get(0).variationIndex());
        assertNull(events.get(1).flagVersion());
        assertNull(events.get(1).variationIndex());
      }
    }
  }

  private static void assertSame(Object expected, Object actual) {
    org.junit.jupiter.api.Assertions.assertSame(expected, actual);
  }

  @Test
  void countsEitherSideOfUtcMidnightGoOutAsTwoDays() throws Exception {
    try (FakeServer server = new FakeServer()) {
      serve(server, FakeServer.ruleset(FakeServer.flag("b", 1, "false", "true")));
      AtomicReference<Instant> now =
          new AtomicReference<>(Instant.parse("2026-11-10T23:59:59.900Z"));
      Clock clock =
          new Clock() {
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
          };
      try (ToggleSlashClient client = poll(server, options(server).clock(clock))) {
        assertTrue(client.ready());
        client.boolVariation("b", USER, false);
        now.set(Instant.parse("2026-11-11T00:00:00.100Z"));
        client.boolVariation("b", USER, false);
        client.boolVariation("b", USER, false);
        client.flushNow();
      }
      List<FakeServer.Seen> posts = server.to("/sdk/v1/events");
      assertEquals(1, posts.size());
      @SuppressWarnings("unchecked")
      Map<String, Object> body = (Map<String, Object>) Json.parse(posts.get(0).body());
      List<?> events = (List<?>) body.get("events");
      assertEquals(2, events.size());
      assertEquals(
          Map.of("flag", "b", "variationIndex", 1.0, "count", 1.0, "day", "2026-11-10"),
          events.get(0));
      assertEquals(
          Map.of("flag", "b", "variationIndex", 1.0, "count", 2.0, "day", "2026-11-11"),
          events.get(1));
    }
  }

  private static Clock fixed() {
    return Clock.fixed(Instant.parse("2026-11-10T12:00:00Z"), ZoneOffset.UTC);
  }

  @Test
  void aRetryAfterAboveThreeHundredIsHeldForThreeHundredOnTheRulesetRoute() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.on("/sdk/v1/ruleset", x -> FakeServer.reply(x, 429, "", "Retry-After", "100000"));
      Clock clock = fixed();
      try (ToggleSlashClient client = poll(server, options(server).clock(clock))) {
        assertFalse(client.ready());
        assertEquals("http_429", client.status().lastError());
        assertEquals(clock.millis() + 300_000L, client.rulesetHoldUntil());
      }
    }
  }

  @Test
  void aRetryAfterIsReadOnlyAsWholeSeconds() throws Exception {
    for (String[] c :
        new String[][] {
          {"7", "7000"},
          {" 12 ", "12000"},
          {"0007", "7000"},
          {"1.5", "0"},
          {"-3", "0"},
          {"Wed, 21 Oct 2026 07:28:00 GMT", "0"},
          {"", "0"},
          {"99999999999999999999", "300000"}
        }) {
      try (FakeServer server = new FakeServer()) {
        server.on("/sdk/v1/ruleset", x -> FakeServer.reply(x, 429, "", "Retry-After", c[0]));
        Clock clock = fixed();
        try (ToggleSlashClient client = poll(server, options(server).clock(clock))) {
          assertFalse(client.ready());
          long expected = c[1].equals("0") ? 0L : clock.millis() + Long.parseLong(c[1]);
          assertEquals(expected, client.rulesetHoldUntil(), "Retry-After: " + c[0]);
        }
      }
    }
  }

  @Test
  void aRetryAfterAboveThreeHundredIsHeldForThreeHundredOnTheStreamRoute() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.on(
          "/sdk/v1/ruleset", x -> FakeServer.reply(x, 200, FakeServer.ruleset(), "ETag", "\"v1\""));
      server.on("/sdk/v1/stream", x -> FakeServer.reply(x, 429, "", "Retry-After", "100000"));
      Clock clock = fixed();
      try (ToggleSlashClient client = poll(server, options(server).stream(true).clock(clock))) {
        assertTrue(client.ready());
        assertTrue(Wait.until(5000, () -> client.rulesetHoldUntil() > 0));
        assertEquals(clock.millis() + 300_000L, client.rulesetHoldUntil());
        assertEquals("http_429", client.status().lastError());
      }
    }
  }

  @Test
  void aRetryAfterAboveThreeHundredIsHeldForThreeHundredOnTheEventsRoute() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.on(
          "/sdk/v1/ruleset",
          x ->
              FakeServer.reply(
                  x,
                  200,
                  FakeServer.ruleset(FakeServer.flag("b", 1, "false", "true")),
                  "ETag",
                  "\"v1\""));
      server.on("/sdk/v1/events", x -> FakeServer.reply(x, 429, "", "Retry-After", "99999999999"));
      Clock clock = fixed();
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      try (ToggleSlashClient client =
          poll(server, options(server).clock(clock).onError(errors::add))) {
        assertTrue(client.ready());
        client.boolVariation("b", USER, false);
        client.flushNow();
        assertEquals(clock.millis() + 300_000L, client.usageHoldUntil());
        client.flushNow();
        assertEquals(1, server.to("/sdk/v1/events").size());
        assertEquals(1, errors.size());
      }
      assertEquals(1, server.to("/sdk/v1/events").size());
    }
  }

  @Test
  void usageStatusRulesKeepOrDropByAnswer() throws Exception {
    for (int status : new int[] {500, 408, 400, 404, 413}) {
      try (FakeServer server = new FakeServer()) {
        List<Integer> answers = new CopyOnWriteArrayList<>(List.of(status));
        server.on(
            "/sdk/v1/ruleset",
            x ->
                FakeServer.reply(
                    x,
                    200,
                    FakeServer.ruleset(FakeServer.flag("b", 1, "false", "true")),
                    "ETag",
                    "\"v1\""));
        server.on(
            "/sdk/v1/events",
            x -> FakeServer.reply(x, answers.isEmpty() ? 200 : answers.remove(0), "{}"));
        try (ToggleSlashClient client = poll(server, options(server))) {
          assertTrue(client.ready());
          client.boolVariation("b", USER, false);
          client.flushNow();
          client.flushNow();
          boolean kept = status >= 500 || status == 408;
          assertEquals(kept ? 2 : 1, server.to("/sdk/v1/events").size(), "status " + status);
        }
      }
    }
  }

  @Test
  void daemonThreadsDoNotKeepTheProcessAlive() throws Exception {
    try (FakeServer server = new FakeServer()) {
      serve(server, FakeServer.ruleset());
      ToggleSlashClient client = poll(server, options(server));
      assertTrue(client.ready());
      Set<String> seen = new TreeSet<>();
      for (Thread t : Thread.getAllStackTraces().keySet()) {
        if (t.getName().startsWith("toggleslash-")) {
          seen.add(t.getName());
          assertTrue(t.isDaemon(), t.getName());
        }
      }
      assertTrue(seen.contains("toggleslash-sync"));
      assertTrue(seen.contains("toggleslash-flush"));
      client.close();
    }
  }

  @Test
  void theOnlyHeadersTheSdkSetAreTheProtocolsAndTheTableIsPrinted() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.on(
          "/sdk/v1/ruleset",
          x -> {
            if ("\"v1\"".equals(x.getRequestHeaders().getFirst("If-None-Match"))) {
              FakeServer.reply(x, 304, "", "ETag", "\"v1\"");
            } else {
              FakeServer.reply(
                  x,
                  200,
                  FakeServer.ruleset(FakeServer.flag("b", 1, "false", "true")),
                  "ETag",
                  "\"v1\"");
            }
          });
      server.on("/sdk/v1/events", x -> FakeServer.reply(x, 200, "{}"));
      server.on(
          "/sdk/v1/stream",
          x -> {
            java.io.OutputStream out = FakeServer.open(x);
            FakeServer.write(out, "event: connected\ndata: \"v1\"\n\n");
            FakeServer.hang();
          });
      try (ToggleSlashClient poller =
              poll(server, options(server).pollInterval(Duration.ofMillis(50)));
          ToggleSlashClient streamer = poll(server, options(server).stream(true))) {
        assertTrue(poller.ready());
        assertTrue(streamer.ready());
        poller.boolVariation("b", USER, false);
        poller.flushNow();
        assertTrue(
            Wait.until(
                5000,
                () ->
                    server.seen.stream().anyMatch(s -> s.headers().containsKey("If-none-match"))
                        && !server.to("/sdk/v1/stream").isEmpty()));
      }
      Set<String> allowed = Set.of("authorization", "accept", "if-none-match", "content-type");
      Set<String> byClient =
          Set.of(
              "user-agent",
              "host",
              "content-length",
              "connection",
              "accept-encoding",
              "upgrade",
              "http2-settings");
      Map<String, Set<String>> table = new java.util.LinkedHashMap<>();
      for (FakeServer.Seen s : server.seen) {
        String route =
            s.path().substring("/sdk/v1/".length())
                + (s.headers().containsKey("If-none-match") ? " (conditional)" : "");
        Set<String> names = table.computeIfAbsent(route, k -> new TreeSet<>());
        for (String name : s.headers().keySet()) {
          String lower = name.toLowerCase(java.util.Locale.ROOT);
          names.add(lower);
          assertTrue(
              allowed.contains(lower) || byClient.contains(lower), "unexpected header " + lower);
        }
        assertEquals("Bearer fs_server_test", s.headers().get("Authorization").get(0));
      }
      for (Map.Entry<String, Set<String>> row : table.entrySet()) {
        System.out.println("headers: " + row.getKey() + ": " + String.join(", ", row.getValue()));
      }
      assertTrue(table.containsKey("ruleset"));
      assertTrue(table.containsKey("ruleset (conditional)"));
      assertTrue(table.containsKey("stream"));
      assertTrue(table.containsKey("events"));
      List<String> unused = new ArrayList<>();
      assertNotNull(unused);
    }
  }
}
