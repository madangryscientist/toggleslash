package com.github.madangryscientist.toggleslash.sdks.java;

import static com.github.madangryscientist.toggleslash.sdks.java.Support.KEY;
import static com.github.madangryscientist.toggleslash.sdks.java.Support.base;
import static com.github.madangryscientist.toggleslash.sdks.java.Support.until;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.FakeServer.Req;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ReasonKind;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;

class ClientTest {
  private static final String BOOLS = "[false,true]";
  private static final Context USER = Context.user("u1");

  private static String boolFlag(String key, long version) {
    return FakeServer.flag(key, version, BOOLS, 1);
  }

  @Test
  void servesFromTheLoadedRuleset() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 3));
      try (Client client = Client.create(base(server).build())) {
        assertTrue(client.ready());
        assertTrue(client.boolVariation("b", USER, false));
        Detail<Boolean> detail = client.boolVariationDetail("b", USER, false);
        assertEquals(1, detail.variationIndex());
        assertEquals(ReasonKind.FALLTHROUGH, detail.reason().kind());
        Status status = client.status();
        assertEquals("\"v1\"", status.etag());
        assertNotNull(status.lastSyncAt());
        assertNull(status.lastError());
        assertEquals(StreamState.OFF, status.stream());
        Detail<Boolean> missing = client.boolVariationDetail("nope", USER, true);
        assertTrue(missing.value());
        assertNull(missing.variationIndex());
        assertEquals(ErrorCode.FLAG_NOT_FOUND, missing.reason().errorCode());
      }
    }
  }

  @Test
  void sendsIfNoneMatchOnlyWithARulesetHeld() throws Exception {
    try (FakeServer server = new FakeServer()) {
      try (Client client =
          Client.create(base(server).pollInterval(Duration.ofMillis(30)).build())) {
        assertTrue(client.ready());
        assertTrue(server.await("/sdk/v1/ruleset", 3, 5000));
        List<Req> asked = server.to("/sdk/v1/ruleset");
        assertNull(asked.get(0).header("if-none-match"));
        assertEquals("\"v1\"", asked.get(1).header("if-none-match"));
        assertEquals("\"v1\"", asked.get(2).header("if-none-match"));
        assertEquals("\"v1\"", client.status().etag());
        assertNull(client.status().lastError());
      }
    }
  }

  @Test
  void answersFallbacksWhileTheRulesetHangs() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetGate = new CountDownLatch(1);
      Options options = base(server).requestTimeout(Duration.ofMillis(300)).build();
      try (Client client = Client.create(options)) {
        long start = System.nanoTime();
        Detail<Boolean> early = client.boolVariationDetail("b", USER, true);
        assertTrue(System.nanoTime() - start < 200_000_000L, "variation must not wait");
        assertTrue(early.value());
        assertEquals(ErrorCode.NOT_READY, early.reason().errorCode());
        assertFalse(client.ready());
        assertEquals("timeout", client.status().lastError());
        assertNull(client.status().etag());
        // ready is fixed after the first attempt, even when a later poll works
        server.rulesetGate.countDown();
        assertTrue(until(5000, () -> client.status().etag() != null));
        assertFalse(client.ready());
      }
    }
  }

  @Test
  void aBadRulesetNeverReplacesAGoodOne() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      try (Client client =
          Client.create(base(server).pollInterval(Duration.ofMillis(30)).build())) {
        assertTrue(client.ready());
        for (String bad : List.of("{}", "[]", "not json", "{\"flags\":{},\"segments\":[]}")) {
          server.serve("\"bad\"", bad);
          int seen = server.count("/sdk/v1/ruleset");
          assertTrue(server.await("/sdk/v1/ruleset", seen + 2, 5000));
          assertEquals("bad_ruleset", client.status().lastError(), bad);
          assertEquals("\"v1\"", client.status().etag(), bad);
          assertTrue(client.boolVariation("b", USER, false), bad);
        }
      }
    }
  }

  @Test
  void aFirstBadRulesetLeavesTheClientNotReady() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = "{}";
      try (Client client = Client.create(base(server).build())) {
        assertFalse(client.ready());
        assertEquals("bad_ruleset", client.status().lastError());
        assertEquals(
            ErrorCode.NOT_READY, client.boolVariationDetail("b", USER, false).reason().errorCode());
      }
    }
  }

  @Test
  void aServerErrorIsNamedAndKeepsWhatIsHeld() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Options options =
          base(server).pollInterval(Duration.ofMillis(30)).onError(errors::add).build();
      try (Client client = Client.create(options)) {
        assertTrue(client.ready());
        server.rulesetStatus = 503;
        assertTrue(until(5000, () -> "http_503".equals(client.status().lastError())));
        assertTrue(client.boolVariation("b", USER, false));
        assertFalse(errors.isEmpty());
        server.rulesetStatus = 200;
        assertTrue(until(5000, () -> client.status().lastError() == null));
      }
    }
  }

  @Test
  void aRefusedConnectionIsNetwork() throws Exception {
    int port;
    try (FakeServer gone = new FakeServer()) {
      port = gone.port();
    }
    Options options =
        Options.builder().url("http://127.0.0.1:" + port).sdkKey(KEY).stream(false)
            .requestTimeout(Duration.ofSeconds(2))
            .build();
    try (Client client = Client.create(options)) {
      assertFalse(client.ready());
      assertEquals("network", client.status().lastError());
    }
  }

  @Test
  void headerAudit() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      Options options = base(server).stream(true).pollInterval(Duration.ofSeconds(5)).build();
      server.event("connected", "\"v1\"");
      try (Client client = Client.create(options)) {
        assertTrue(client.ready());
        assertTrue(until(5000, () -> client.status().stream() == StreamState.CONNECTED));
        assertTrue(client.boolVariation("b", USER, false));
        server.serve("\"v2\"", FakeServer.ruleset(boolFlag("b", 2)));
        server.event("changed", "\"v2\"");
        assertTrue(until(5000, () -> "\"v2\"".equals(client.status().etag())));
        client.flush();
      }
      Set<String> allowed =
          Set.of(
              "authorization",
              "accept",
              "if-none-match",
              "content-type",
              "host",
              "user-agent",
              "content-length",
              "connection");
      Map<String, Set<String>> table = new TreeMap<>();
      for (Req req : server.requests) {
        String route = req.method() + " " + req.path();
        String listed =
            req.header("if-none-match") == null ? route : route + " (with If-None-Match)";
        table
            .computeIfAbsent(listed, k -> new java.util.TreeSet<>())
            .addAll(req.headers().keySet());
        assertEquals("Bearer " + KEY, req.header("authorization"), route);
        for (String name : req.headers().keySet()) {
          assertTrue(allowed.contains(name), route + " sent " + name);
        }
        assertFalse(req.body().contains("u1"), route + " must not carry the context");
      }
      assertEquals(
          Set.of(
              "GET /sdk/v1/ruleset",
              "GET /sdk/v1/ruleset (with If-None-Match)",
              "GET /sdk/v1/stream",
              "POST /sdk/v1/events"),
          table.keySet());
      assertEquals("text/event-stream", server.to("/sdk/v1/stream").get(0).header("accept"));
      assertEquals("application/json", server.to("/sdk/v1/events").get(0).header("content-type"));
      assertNull(server.to("/sdk/v1/ruleset").get(0).header("content-type"));
      table.forEach((route, names) -> System.out.println("headers " + route + ": " + names));
      System.out.println("user-agent " + server.requests.get(0).header("user-agent"));
    }
  }

  @Test
  void theStreamAnnouncesAndAChangedEventFetches() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(FakeServer.flag("s", 1, "[\"a\",\"b\"]", 0));
      Options options = base(server).stream(true).pollInterval(Duration.ofSeconds(30)).build();
      try (Client client = Client.create(options)) {
        assertTrue(client.ready());
        server.event("connected", "\"v1\"");
        assertTrue(until(5000, () -> client.status().stream() == StreamState.CONNECTED));
        assertEquals(1, server.count("/sdk/v1/ruleset"), "a matching tag fetches nothing");
        assertEquals("a", client.stringVariation("s", USER, "z"));
        server.serve("\"v2\"", FakeServer.ruleset(FakeServer.flag("s", 2, "[\"a\",\"b\"]", 1)));
        server.event("changed", "\"v2\"");
        assertTrue(until(5000, () -> "b".equals(client.stringVariation("s", USER, "z"))));
        assertEquals("\"v2\"", client.status().etag());
        assertEquals(2, server.count("/sdk/v1/ruleset"));
        server.event("changed", "\"v2\"");
        server.event("changed", "\"v2\"");
        Thread.sleep(150);
        assertEquals(2, server.count("/sdk/v1/ruleset"), "an unchanged tag fetches nothing");
      }
    }
  }

  @Test
  void aStreamThatEndsIsFollowedByAReconnect() throws Exception {
    try (FakeServer server = new FakeServer()) {
      Options options = base(server).stream(true).pollInterval(Duration.ofMillis(100)).build();
      server.event("connected", "\"v1\"");
      server.streamOut.add(FakeServer.END);
      try (Client client = Client.create(options)) {
        assertTrue(client.ready());
        assertTrue(server.await("/sdk/v1/stream", 2, 5000));
        server.event("connected", "\"v1\"");
        assertTrue(until(5000, () -> client.status().stream() == StreamState.CONNECTED));
      }
    }
  }

  @Test
  void aQuietStreamIsDroppedByTheWatchdogAndAKeepAliveHoldsIt() throws Exception {
    try (FakeServer server = new FakeServer()) {
      Options options =
          base(server).stream(true)
              .pollInterval(Duration.ofMillis(100))
              .streamIdleTimeout(Duration.ofMillis(600))
              .build();
      server.event("connected", "\"v1\"");
      try (Client client = Client.create(options)) {
        assertTrue(client.ready());
        assertTrue(until(5000, () -> client.status().stream() == StreamState.CONNECTED));
        for (int i = 0; i < 10; i++) {
          server.streamOut.add(": keep-alive\n\n");
          Thread.sleep(200);
        }
        assertEquals(1, server.count("/sdk/v1/stream"), "keep-alives restart the watchdog");
        assertEquals(StreamState.CONNECTED, client.status().stream());
        assertTrue(until(5000, () -> "timeout".equals(client.status().lastError())));
        assertTrue(server.await("/sdk/v1/stream", 2, 5000));
      }
    }
  }

  @Test
  void aStreamAnswerOtherThan200IsNamedAndFallsBackToPolling() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.streamStatus = 401;
      Options options = base(server).stream(true).pollInterval(Duration.ofMillis(50)).build();
      try (Client client = Client.create(options)) {
        assertTrue(client.ready());
        assertTrue(until(5000, () -> "http_401".equals(client.status().lastError())));
        assertTrue(server.await("/sdk/v1/ruleset", 3, 5000), "polling carries on");
        assertTrue(client.status().stream() != StreamState.CONNECTED);
      }
    }
  }

  @Test
  void aLongRetryAfterIsCappedAndHeldOnEveryRoute() throws Exception {
    // ruleset
    try (FakeServer server = new FakeServer()) {
      server.rulesetStatus = 429;
      server.rulesetRetryAfter = "99999";
      TestClock clock = new TestClock(1_000_000L);
      try (Client client = Client.create(base(server).clock(clock).build())) {
        assertFalse(client.ready());
        assertEquals("http_429", client.status().lastError());
        assertTrue(clock.awaitSleepers(1, 5000));
        assertEquals(300_000L, clock.longestWait());
        Thread.sleep(100);
        assertEquals(1, server.count("/sdk/v1/ruleset"));
        server.rulesetStatus = 200;
        clock.advance(299_999L);
        Thread.sleep(100);
        assertEquals(1, server.count("/sdk/v1/ruleset"));
        clock.advance(1L);
        assertTrue(server.await("/sdk/v1/ruleset", 2, 5000));
      }
    }
    // stream
    try (FakeServer server = new FakeServer()) {
      server.streamStatus = 429;
      server.streamRetryAfter = "120";
      TestClock clock = new TestClock(1_000_000L);
      try (Client client = Client.create(base(server).stream(true).clock(clock).build())) {
        assertTrue(client.ready());
        assertTrue(server.await("/sdk/v1/stream", 1, 5000));
        assertTrue(until(5000, () -> clock.longestWait() == 120_000L));
        Thread.sleep(100);
        assertEquals(1, server.count("/sdk/v1/stream"));
        server.streamStatus = 200;
        server.event("connected", "\"v1\"");
        clock.advance(120_000L);
        assertTrue(until(5000, () -> client.status().stream() == StreamState.CONNECTED));
      }
    }
    // events
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      server.eventsStatus = 429;
      server.eventsRetryAfter = "400";
      TestClock clock = new TestClock(1_000_000L);
      try (Client client = Client.create(base(server).clock(clock).build())) {
        assertTrue(client.ready());
        client.boolVariation("b", USER, false);
        client.flush();
        assertEquals(1, server.count("/sdk/v1/events"));
        client.flush();
        assertEquals(1, server.count("/sdk/v1/events"), "held for the Retry-After");
        server.eventsStatus = 204;
        clock.advance(299_999L);
        client.flush();
        assertEquals(1, server.count("/sdk/v1/events"), "the cap is 300 s, not less");
        clock.advance(1L);
        client.flush();
        assertEquals(2, server.count("/sdk/v1/events"));
        Map<?, ?> body = (Map<?, ?>) Json.parse(server.to("/sdk/v1/events").get(1).body());
        assertEquals(1.0, ((Map<?, ?>) ((List<?>) body.get("events")).get(0)).get("count"));
      }
    }
  }

  @Test
  void usageBodyHasCountsAndNothingAboutTheUser() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      TestClock clock = new TestClock(Instant.parse("2026-03-05T10:00:00Z").toEpochMilli());
      try (Client client = Client.create(base(server).clock(clock).build())) {
        assertTrue(client.ready());
        for (int i = 0; i < 3; i++) {
          client.boolVariation("b", Context.user("someone-" + i), false);
        }
        client.boolVariation("missing", USER, false);
        client.flush();
        assertEquals(1, server.count("/sdk/v1/events"));
        Map<?, ?> body = (Map<?, ?>) Json.parse(server.to("/sdk/v1/events").get(0).body());
        assertEquals(Set.of("events"), body.keySet());
        List<?> events = (List<?>) body.get("events");
        assertEquals(1, events.size(), "a flag that is not served is not counted");
        Map<?, ?> event = (Map<?, ?>) events.get(0);
        assertEquals(Set.of("flag", "variationIndex", "day", "count"), event.keySet());
        assertEquals("b", event.get("flag"));
        assertEquals(1.0, event.get("variationIndex"));
        assertEquals("2026-03-05", event.get("day"));
        assertEquals(3.0, event.get("count"));
        assertFalse(server.to("/sdk/v1/events").get(0).body().contains("someone"));
        client.flush();
        assertEquals(1, server.count("/sdk/v1/events"), "nothing pending sends nothing");
      }
    }
  }

  @Test
  void usageSplitsAtUtcMidnight() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      TestClock clock = new TestClock(Instant.parse("2026-12-31T23:59:59Z").toEpochMilli());
      try (Client client = Client.create(base(server).clock(clock).build())) {
        assertTrue(client.ready());
        client.boolVariation("b", USER, false);
        clock.advance(2000);
        client.boolVariation("b", USER, false);
        client.boolVariation("b", USER, false);
        client.flush();
        Map<?, ?> body = (Map<?, ?>) Json.parse(server.to("/sdk/v1/events").get(0).body());
        Map<String, Object> byDay = new TreeMap<>();
        for (Object e : (List<?>) body.get("events")) {
          byDay.put((String) ((Map<?, ?>) e).get("day"), ((Map<?, ?>) e).get("count"));
        }
        assertEquals(Map.of("2026-12-31", 1.0, "2027-01-01", 2.0), byDay);
      }
    }
  }

  @Test
  void usageIsSentInBatchesOfAtMost500() throws Exception {
    try (FakeServer server = new FakeServer()) {
      List<String> flags = new ArrayList<>();
      for (int i = 0; i < 1201; i++) {
        flags.add(boolFlag("f" + i, 1));
      }
      server.rulesetBody = FakeServer.ruleset(flags.toArray(String[]::new));
      try (Client client = Client.create(base(server).build())) {
        assertTrue(client.ready());
        for (int i = 0; i < 1201; i++) {
          client.boolVariation("f" + i, USER, false);
        }
        client.flush();
        List<Req> posts = server.to("/sdk/v1/events");
        assertEquals(3, posts.size());
        int total = 0;
        for (Req post : posts) {
          int n = ((List<?>) ((Map<?, ?>) Json.parse(post.body())).get("events")).size();
          assertTrue(n <= 500);
          total += n;
        }
        assertEquals(1201, total);
      }
    }
  }

  @Test
  void aServerErrorKeepsTheCountsAndABadRequestDropsThem() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      try (Client client = Client.create(base(server).build())) {
        assertTrue(client.ready());
        client.boolVariation("b", USER, false);
        server.eventsStatus = 500;
        client.flush();
        assertEquals(1, server.count("/sdk/v1/events"));
        client.boolVariation("b", USER, false);
        server.eventsStatus = 204;
        client.flush();
        assertEquals(2, server.count("/sdk/v1/events"));
        Map<?, ?> kept = (Map<?, ?>) Json.parse(server.to("/sdk/v1/events").get(1).body());
        assertEquals(2.0, ((Map<?, ?>) ((List<?>) kept.get("events")).get(0)).get("count"));

        client.boolVariation("b", USER, false);
        server.eventsStatus = 400;
        client.flush();
        assertEquals(3, server.count("/sdk/v1/events"));
        server.eventsStatus = 204;
        client.flush();
        assertEquals(3, server.count("/sdk/v1/events"), "a 400 is dropped, not retried");
      }
    }
  }

  @Test
  void closeSendsTheLastCountsAndIsIdempotent() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      Client client = Client.create(base(server).build());
      assertTrue(client.ready());
      client.boolVariation("b", USER, false);
      client.close();
      client.close();
      assertEquals(1, server.count("/sdk/v1/events"));
      assertEquals(StreamState.CLOSED, client.status().stream());
      assertTrue(client.boolVariation("b", USER, false), "evaluation still works after close");
      client.flush();
      assertEquals(1, server.count("/sdk/v1/events"), "nothing is recorded after close");
      assertNull(client.status().lastError());
    }
  }

  @Test
  void concurrentClosesAllReturnAfterTheFinalFlush() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      Client client = Client.create(base(server).build());
      assertTrue(client.ready());
      client.boolVariation("b", USER, false);
      ExecutorService pool = Executors.newFixedThreadPool(8);
      try {
        List<Future<Integer>> done = new ArrayList<>();
        for (int i = 0; i < 8; i++) {
          done.add(
              pool.submit(() -> {
                client.close();
                return server.count("/sdk/v1/events");
              }));
        }
        for (Future<Integer> f : done) {
          assertEquals(1, f.get(10, TimeUnit.SECONDS));
        }
      } finally {
        pool.shutdownNow();
      }
    }
  }

  @Test
  void closeIsBoundedWhenARequestHangs() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetGate = new CountDownLatch(1);
      Client client = Client.create(base(server).requestTimeout(Duration.ofSeconds(2)).build());
      assertTrue(server.await("/sdk/v1/ruleset", 1, 5000));
      long start = System.nanoTime();
      client.close();
      assertTrue(
          System.nanoTime() - start < 4_000_000_000L, "close is bounded by the request timeout");
      assertFalse(client.ready());
    }
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      server.eventsGate = new CountDownLatch(1);
      Client client = Client.create(base(server).requestTimeout(Duration.ofMillis(500)).build());
      assertTrue(client.ready());
      client.boolVariation("b", USER, false);
      long start = System.nanoTime();
      client.close();
      assertTrue(System.nanoTime() - start < 3_000_000_000L);
    }
  }

  @Test
  void closeDuringReadyReleasesReady() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetGate = new CountDownLatch(1);
      Client client = Client.create(base(server).requestTimeout(Duration.ofSeconds(5)).build());
      assertTrue(server.await("/sdk/v1/ruleset", 1, 5000));
      ExecutorService pool = Executors.newSingleThreadExecutor();
      try {
        Future<Boolean> ready = pool.submit(client::ready);
        Thread.sleep(100);
        client.close();
        assertFalse(ready.get(5, TimeUnit.SECONDS));
      } finally {
        pool.shutdownNow();
      }
    }
  }

  @Test
  void theHookSeesTheEtagAndVersionOfTheSameRuleset() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.serve("\"v1\"", FakeServer.ruleset(boolFlag("b", 1)));
      List<EvaluationEvent> seen = new CopyOnWriteArrayList<>();
      Options options =
          base(server).pollInterval(Duration.ofMillis(3)).afterEvaluation(seen::add).build();
      AtomicBoolean stop = new AtomicBoolean();
      Thread swapper =
          new Thread(() -> {
            int n = 1;
            while (!stop.get()) {
              n = n % 5 + 1;
              server.serve("\"v" + n + "\"", FakeServer.ruleset(boolFlag("b", n)));
              try {
                Thread.sleep(2);
              } catch (InterruptedException e) {
                return;
              }
            }
          });
      try (Client client = Client.create(options)) {
        assertTrue(client.ready());
        swapper.start();
        long end = System.nanoTime() + 1_500_000_000L;
        while (System.nanoTime() < end) {
          client.boolVariation("b", USER, false);
        }
      } finally {
        stop.set(true);
        swapper.join();
      }
      assertTrue(seen.size() > 100);
      Set<String> etags = new java.util.TreeSet<>();
      for (EvaluationEvent e : seen) {
        assertEquals("\"v" + e.flagVersion() + "\"", e.etag());
        assertEquals("b", e.flagKey());
        assertSame(USER, e.context());
        etags.add(e.etag());
      }
      assertTrue(etags.size() > 1, "the ruleset really changed under the loop");
    }
  }

  private static void assertSame(Object expected, Object actual) {
    org.junit.jupiter.api.Assertions.assertSame(expected, actual);
  }

  @Test
  void theHookGetsFallbacksTooAndAThrowingHookChangesNothing() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 7));
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      List<EvaluationEvent> seen = new CopyOnWriteArrayList<>();
      AfterEvaluation hook =
          event -> {
            seen.add(event);
            throw new IllegalStateException("hook boom");
          };
      Options options = base(server).afterEvaluation(hook).onError(errors::add).build();
      try (Client client = Client.create(options)) {
        assertTrue(client.ready());
        assertTrue(client.boolVariation("b", USER, false));
        assertTrue(client.boolVariation("nope", USER, true));
        assertEquals(2, seen.size());
        assertEquals(7L, seen.get(0).flagVersion());
        assertEquals(Boolean.TRUE, seen.get(0).value());
        assertEquals(1, seen.get(0).variationIndex());
        assertNull(seen.get(1).flagVersion());
        assertNull(seen.get(1).variationIndex());
        assertEquals(ErrorCode.FLAG_NOT_FOUND, seen.get(1).reason().errorCode());
        assertEquals(2, errors.size());
        assertEquals("hook boom", errors.get(0).getMessage());
      }
    }
  }

  @Test
  void theHookRunsBeforeReadyWithNotReady() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetGate = new CountDownLatch(1);
      List<EvaluationEvent> seen = new CopyOnWriteArrayList<>();
      Options options =
          base(server).requestTimeout(Duration.ofMillis(300)).afterEvaluation(seen::add).build();
      try (Client client = Client.create(options)) {
        client.boolVariation("b", USER, false);
        assertEquals(1, seen.size());
        assertNull(seen.get(0).etag());
        assertNull(seen.get(0).flagVersion());
        assertEquals(ErrorCode.NOT_READY, seen.get(0).reason().errorCode());
      }
    }
  }

  @Test
  void anOnErrorThatThrowsIsIgnored() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetStatus = 500;
      AtomicInteger calls = new AtomicInteger();
      Options options =
          base(server)
              .onError(e -> {
                calls.incrementAndGet();
                throw new IllegalStateException("onError boom");
              })
              .build();
      try (Client client = Client.create(options)) {
        assertFalse(client.ready());
        assertTrue(until(2000, () -> calls.get() > 0));
        assertTrue(client.boolVariation("b", USER, true));
      }
    }
  }

  @Test
  void aHookMayCallBackIntoTheClient() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      AtomicReference<Client> holder = new AtomicReference<>();
      AtomicInteger depth = new AtomicInteger();
      AtomicInteger inner = new AtomicInteger();
      AfterEvaluation hook =
          event -> {
            holder.get().status();
            if (depth.getAndIncrement() == 0) {
              holder.get().boolVariation("b", USER, false);
              holder.get().flush();
              inner.incrementAndGet();
            }
          };
      Client client = Client.create(base(server).afterEvaluation(hook).build());
      holder.set(client);
      ExecutorService pool = Executors.newSingleThreadExecutor();
      try {
        Future<Boolean> done =
            pool.submit(() -> {
              client.ready();
              client.boolVariation("b", USER, false);
              client.close();
              return true;
            });
        assertTrue(done.get(10, TimeUnit.SECONDS));
        assertEquals(1, inner.get());
      } finally {
        pool.shutdownNow();
      }
    }
  }

  @Test
  void readyAndCloseFromACallbackOnAnSdkThreadReturnAtOnce() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetStatus = 500;
      AtomicReference<Client> holder = new AtomicReference<>();
      CountDownLatch finished = new CountDownLatch(1);
      AtomicReference<Throwable> failure = new AtomicReference<>();
      Options options =
          base(server)
              .requestTimeout(Duration.ofSeconds(30))
              .onError(e -> {
                if (holder.get() == null || finished.getCount() == 0) {
                  return;
                }
                try {
                  holder.get().ready();
                  holder.get().close();
                  holder.get().close();
                } catch (Throwable t) {
                  failure.set(t);
                } finally {
                  finished.countDown();
                }
              })
              .build();
      Client client = Client.create(options);
      holder.set(client);
      assertTrue(
          finished.await(10, TimeUnit.SECONDS), "a callback that closes must not wait for itself");
      assertNull(failure.get());
      client.close();
    }
  }

  @Test
  void anEvaluatorFailureIsOneOnErrorAndTheFallback() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Client.Evaluate broken =
          (ruleset, key, context) -> {
            throw new IllegalArgumentException("evaluator boom");
          };
      Options options = base(server).onError(errors::add).build();
      try (Client client = Client.create(options, broken)) {
        assertTrue(client.ready());
        Detail<Boolean> detail = client.boolVariationDetail("b", USER, true);
        assertTrue(detail.value());
        assertNull(detail.variationIndex());
        assertEquals(ErrorCode.EVALUATION_FAILED, detail.reason().errorCode());
        assertEquals(1, errors.size());
        client.flush();
        assertEquals(0, server.count("/sdk/v1/events"));
      }
    }
  }

  @Test
  void aServedValueOfTheWrongTypeIsAMismatchAndNotCounted() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody =
          FakeServer.ruleset(
              FakeServer.flag("s", 1, "[\"str\"]", 0), FakeServer.flag("n", 1, "[1.5]", 0));
      try (Client client = Client.create(base(server).build())) {
        assertTrue(client.ready());
        Detail<Boolean> asBool = client.boolVariationDetail("s", USER, true);
        assertTrue(asBool.value());
        assertNull(asBool.variationIndex());
        assertEquals(ErrorCode.TYPE_MISMATCH, asBool.reason().errorCode());
        assertEquals("fb", client.stringVariation("n", USER, "fb"));
        assertEquals(7.0, client.numberVariation("s", USER, 7.0));
        client.flush();
        assertEquals(0, server.count("/sdk/v1/events"));
      }
    }
  }

  @Test
  void theIntegerAccessorNeedsAWholeNumberWithin64Bits() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody =
          FakeServer.ruleset(
              FakeServer.flag("two", 1, "[2.0]", 0),
              FakeServer.flag("frac", 1, "[1.5]", 0),
              FakeServer.flag("edge", 1, "[9223372036854774784]", 0),
              FakeServer.flag("over", 1, "[9223372036854775808]", 0),
              FakeServer.flag("huge", 1, "[1e300]", 0),
              FakeServer.flag("neg", 1, "[-9223372036854775808]", 0),
              FakeServer.flag("zero", 1, "[-0.0]", 0));
      try (Client client = Client.create(base(server).build())) {
        assertTrue(client.ready());
        assertEquals(2L, client.longVariation("two", USER, -1));
        assertEquals(-1L, client.longVariation("frac", USER, -1));
        assertEquals(1.5, client.numberVariation("frac", USER, -1));
        assertEquals(9223372036854774784L, client.longVariation("edge", USER, -1));
        assertEquals(-1L, client.longVariation("over", USER, -1));
        assertEquals(-1L, client.longVariation("huge", USER, -1));
        assertEquals(Long.MIN_VALUE, client.longVariation("neg", USER, -1));
        assertEquals(0L, client.longVariation("zero", USER, -1));
        assertEquals(
            ErrorCode.TYPE_MISMATCH,
            client.longVariationDetail("over", USER, -1).reason().errorCode());
      }
    }
  }

  @Test
  void aNullJsonFallbackAcceptsAnythingAndNullAgainstANonNullFallbackIsAMismatch()
      throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody =
          FakeServer.ruleset(
              FakeServer.flag("null", 1, "[null]", 0),
              FakeServer.flag("obj", 1, "[{\"a\":[1,{\"b\":2}]}]", 0),
              FakeServer.flag("arr", 1, "[[1,2]]", 0),
              FakeServer.flag("str", 1, "[\"s\"]", 0),
              FakeServer.flag("num", 1, "[3]", 0));
      try (Client client = Client.create(base(server).build())) {
        assertTrue(client.ready());
        Detail<Object> nul = client.jsonVariationDetail("null", USER, null);
        assertNull(nul.value());
        assertEquals(0, nul.variationIndex());
        assertEquals("fb", client.jsonVariation("null", USER, "fb"));
        assertEquals(
            ErrorCode.TYPE_MISMATCH,
            client.jsonVariationDetail("null", USER, "fb").reason().errorCode());

        assertEquals(List.of(1.0, 2.0), client.jsonVariation("arr", USER, null));
        assertEquals("s", client.jsonVariation("str", USER, null));
        assertEquals(3.0, client.jsonVariation("num", USER, null));
        assertEquals("fb", client.jsonVariation("obj", USER, "fb"));
        assertEquals(List.of(1.0, 2.0), client.jsonVariation("arr", USER, List.of()));

        Object served = client.jsonVariation("obj", USER, Map.of());
        Map<?, ?> map = (Map<?, ?>) served;
        List<?> inner = (List<?>) map.get("a");
        assertEquals(1.0, inner.get(0));
        assertThrows(UnsupportedOperationException.class, () -> castMap(served).put("x", 1));
        assertThrows(UnsupportedOperationException.class, () -> castList(inner).add(1));
        assertThrows(UnsupportedOperationException.class, () -> castMap(inner.get(1)).put("x", 1));
      }
    }
  }

  @SuppressWarnings("unchecked")
  private static Map<Object, Object> castMap(Object o) {
    return (Map<Object, Object>) o;
  }

  @SuppressWarnings("unchecked")
  private static List<Object> castList(Object o) {
    return (List<Object>) o;
  }

  @Test
  void manyThreadsOnEveryOperationAtOnce() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.serve("\"v1\"", FakeServer.ruleset(boolFlag("b", 1)));
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Options options =
          base(server)
              .pollInterval(Duration.ofMillis(5))
              .flushInterval(Duration.ofMillis(20))
              .onError(errors::add)
              .build();
      Client client = Client.create(options);
      assertTrue(client.ready());
      int threads = 16;
      ExecutorService pool = Executors.newFixedThreadPool(threads + 1);
      AtomicBoolean stop = new AtomicBoolean();
      AtomicInteger wrong = new AtomicInteger();
      try {
        pool.submit(() -> {
          int n = 1;
          while (!stop.get()) {
            n = n % 4 + 1;
            server.serve("\"v" + n + "\"", FakeServer.ruleset(boolFlag("b", n)));
            try {
              Thread.sleep(3);
            } catch (InterruptedException e) {
              return;
            }
          }
        });
        List<Future<?>> done = new ArrayList<>();
        for (int t = 0; t < threads; t++) {
          int id = t;
          done.add(
              pool.submit(() -> {
                for (int i = 0; i < 3000; i++) {
                  if (!client.boolVariation("b", Context.user("t" + id), false)) {
                    wrong.incrementAndGet();
                  }
                  client.status();
                  if (i % 500 == 0) {
                    client.flush();
                  }
                  client.boolVariationDetail("other", USER, true);
                }
              }));
        }
        for (Future<?> f : done) {
          f.get(60, TimeUnit.SECONDS);
        }
      } finally {
        stop.set(true);
        pool.shutdownNow();
      }
      client.close();
      assertEquals(0, wrong.get());
      assertEquals(List.of(), errors);
      long counted = 0;
      for (Req post : server.to("/sdk/v1/events")) {
        for (Object e : (List<?>) ((Map<?, ?>) Json.parse(post.body())).get("events")) {
          counted += ((Double) ((Map<?, ?>) e).get("count")).longValue();
        }
      }
      assertEquals(threads * 3000L, counted, "every served evaluation is counted once");
    }
  }

  @Test
  void closeRacingVariationAndAnotherCloseRaisesNothing() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.serve("\"v1\"", FakeServer.ruleset(boolFlag("b", 1)));
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Client client =
          Client.create(
              base(server).pollInterval(Duration.ofMillis(5)).onError(errors::add).build());
      assertTrue(client.ready());
      int threads = 8;
      ExecutorService pool = Executors.newFixedThreadPool(threads + 2);
      AtomicBoolean stop = new AtomicBoolean();
      AtomicInteger wrong = new AtomicInteger();
      AtomicInteger started = new AtomicInteger();
      try {
        List<Future<?>> workers = new ArrayList<>();
        for (int t = 0; t < threads; t++) {
          workers.add(
              pool.submit(() -> {
                while (!stop.get()) {
                  // The ruleset held answers on both sides of the close.
                  if (!client.boolVariation("b", USER, false)) {
                    wrong.incrementAndGet();
                  }
                  client.boolVariationDetail("b", USER, false);
                  client.status();
                  started.incrementAndGet();
                }
              }));
        }
        // Wait until every worker has made progress, so the close lands in the middle of the calls.
        assertTrue(until(5000, () -> started.get() >= threads * 20));
        Future<?> first = pool.submit(client::close);
        Future<?> second = pool.submit(client::close);
        first.get(15, TimeUnit.SECONDS);
        second.get(15, TimeUnit.SECONDS);
        int after = started.get();
        assertTrue(until(5000, () -> started.get() >= after + threads * 20));
        stop.set(true);
        for (Future<?> f : workers) {
          f.get(15, TimeUnit.SECONDS);
        }
      } finally {
        stop.set(true);
        pool.shutdownNow();
      }
      assertEquals(0, wrong.get());
      assertEquals(StreamState.CLOSED, client.status().stream());
      assertEquals(List.of(), errors);
    }
  }

  @Test
  void theSdkThreadsAreDaemonsAndGoAwayOnClose() throws Exception {
    try (FakeServer server = new FakeServer()) {
      Client client = Client.create(base(server).build());
      assertTrue(client.ready());
      List<Thread> ours = sdkThreads();
      assertFalse(ours.isEmpty());
      for (Thread t : ours) {
        assertTrue(t.isDaemon(), t.getName());
      }
      client.close();
      assertTrue(until(5000, () -> sdkThreads().stream().noneMatch(Thread::isAlive)));
    }
  }

  private static List<Thread> sdkThreads() {
    List<Thread> out = new ArrayList<>();
    for (Thread t : Thread.getAllStackTraces().keySet()) {
      if (t.getName().startsWith("toggleslash-")) {
        out.add(t);
      }
    }
    return out;
  }

  @Test
  void aBadContextAndAnUnknownKindGiveAFallbackNotAThrow() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.rulesetBody = FakeServer.ruleset(boolFlag("b", 1));
      try (Client client = Client.create(base(server).build())) {
        assertTrue(client.ready());
        assertTrue(client.boolVariation("b", null, true));
        Detail<Boolean> bad = client.boolVariationDetail("b", Context.of(Map.of("kind", 5)), false);
        assertFalse(bad.value());
        assertEquals(ErrorCode.INVALID_CONTEXT, bad.reason().errorCode());
        assertTrue(client.boolVariation(null, USER, true));
        assertEquals("fb", client.stringVariation("b", USER, "fb"));
      }
    }
  }
}
