package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ReasonKind;
import java.time.Duration;
import java.time.Instant;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

/**
 * The client against a loopback server: loading, answers, usage, hooks and the contract's edges.
 */
class ClientTest {

  private final FakeServer server = new FakeServer();
  private final List<ToggleSlashClient> clients = new CopyOnWriteArrayList<>();
  private final List<Throwable> errors = new CopyOnWriteArrayList<>();

  @AfterEach
  void tearDown() {
    for (ToggleSlashClient client : clients) {
      client.close();
    }
    server.close();
  }

  static void waitFor(String what, long millis, BooleanSupplier condition) {
    long end = System.nanoTime() + millis * 1_000_000L;
    while (System.nanoTime() < end) {
      if (condition.getAsBoolean()) {
        return;
      }
      try {
        Thread.sleep(5);
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
        throw new IllegalStateException(e);
      }
    }
    throw new AssertionError("timed out waiting for " + what);
  }

  ClientOptions.Builder options() {
    return ClientOptions.builder(server.url(), FakeServer.KEY).stream(false)
        .onError(errors::add)
        .requestTimeout(Duration.ofSeconds(5));
  }

  ToggleSlashClient start(ClientOptions.Builder builder) {
    ToggleSlashClient client = ToggleSlashClient.create(builder.build());
    clients.add(client);
    return client;
  }

  @Test
  void notReadyUntilTheFirstRulesetThenServes() throws Exception {
    CountDownLatch release = new CountDownLatch(1);
    FakeServer.Handler normal = server.rulesetHandler;
    server.rulesetHandler =
        x -> {
          try {
            release.await(5, TimeUnit.SECONDS);
          } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
          }
          normal.handle(x);
        };
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    ToggleSlashClient client = start(options());
    Evaluation<Boolean> before = client.boolVariationDetail("f", Map.of("key", "u"), false);
    assertFalse(before.value());
    assertEquals(ReasonKind.ERROR, before.reason().kind());
    assertEquals(ErrorCode.NOT_READY, before.reason().errorCode());
    assertTrue(before.variationIndex().isEmpty());
    release.countDown();
    assertTrue(client.ready());
    assertTrue(client.boolVariation("f", Map.of("key", "u"), false));
    Status status = client.status();
    assertEquals("\"v1\"", status.etag().orElseThrow());
    assertTrue(status.lastSyncAt().isPresent());
    assertTrue(status.lastError().isEmpty());
    assertEquals(StreamState.OFF, status.stream());
  }

  @Test
  void aPollSendsIfNoneMatchOnlyOnceBothAreHeldAndA304IsASync() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    ToggleSlashClient client = start(options().pollInterval(Duration.ofMillis(40)));
    assertTrue(client.ready());
    waitFor("three ruleset requests", 5000, () -> server.ruleset.size() >= 3);
    assertEquals(null, server.ruleset.get(0).header("If-None-Match"));
    assertEquals("\"v1\"", server.ruleset.get(1).header("If-None-Match"));
    assertEquals("Bearer " + FakeServer.KEY, server.ruleset.get(0).header("Authorization"));
    Instant first = client.status().lastSyncAt().orElseThrow();
    waitFor("a later sync", 5000, () -> client.status().lastSyncAt().orElseThrow().isAfter(first));
    assertTrue(client.status().lastError().isEmpty());
  }

  @Test
  void aBadRulesetKeepsTheLastGoodOne() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    ToggleSlashClient client = start(options().pollInterval(Duration.ofMillis(30)));
    assertTrue(client.ready());
    server.serve("{\"flags\":[]}", "\"v2\"");
    waitFor("bad_ruleset", 5000, () -> client.status().lastError().isPresent());
    assertEquals("bad_ruleset", client.status().lastError().orElseThrow());
    assertEquals("\"v1\"", client.status().etag().orElseThrow());
    assertTrue(client.boolVariation("f", Map.of("key", "u"), false));
    assertFalse(errors.isEmpty());
    ToggleSlashException error = (ToggleSlashException) errors.get(0);
    assertEquals("bad_ruleset", error.code());
  }

  @Test
  void aRefusedFirstLoadSettlesReadyFalseAndReportsTheStatus() {
    server.rulesetHandler = x -> FakeServer.reply(x, 401, "no");
    ToggleSlashClient client = start(options());
    assertFalse(client.ready());
    assertFalse(client.ready());
    assertEquals("http_401", client.status().lastError().orElseThrow());
    assertTrue(client.status().etag().isEmpty());
  }

  @Test
  void accessorsApplyTheKindRules() {
    server.serve(
        FakeServer.rulesetOf(
            FakeServer.flag("big", "9300000000000000000", 1),
            FakeServer.flag("two", "2.0", 1),
            FakeServer.flag("frac", "1.5", 1),
            FakeServer.flag("max", "9223372036854775807", 1),
            FakeServer.flag("neg", "-9223372036854775808", 1),
            FakeServer.flag("text", "\"hi\"", 1),
            FakeServer.flag("nothing", "null", 1),
            FakeServer.flag("list", "[1,2]", 1)),
        "\"v1\"");
    ToggleSlashClient client = start(options());
    assertTrue(client.ready());
    Object ctx = Map.of("key", "u");

    Evaluation<Long> big = client.intVariationDetail("big", ctx, 7);
    assertEquals(7L, big.value());
    assertEquals(ErrorCode.TYPE_MISMATCH, big.reason().errorCode());
    assertTrue(big.variationIndex().isEmpty());
    assertEquals(2L, client.intVariation("two", ctx, 0));
    assertEquals(0L, client.intVariation("frac", ctx, 0));
    assertEquals(1.5, client.doubleVariation("frac", ctx, 0));
    // numbers are doubles, so 2^63 - 1 reads as 2^63, which is outside the signed 64-bit range
    assertEquals(0L, client.intVariation("max", ctx, 0));
    assertEquals(Long.MIN_VALUE, client.intVariation("neg", ctx, 0));
    assertEquals("hi", client.stringVariation("text", ctx, "fb"));
    assertFalse(client.boolVariation("text", ctx, false));
    assertEquals("fb", client.stringVariation("two", ctx, "fb"));
    assertEquals(null, client.stringVariation("nothing", ctx, null));
    assertEquals("fb", client.stringVariation("nothing", ctx, "fb"));
    assertEquals(List.of(1.0, 2.0), client.jsonVariation("list", ctx, null));
    assertEquals(List.of(1.0, 2.0), client.jsonVariation("list", ctx, List.of()));
    assertEquals("x", client.jsonVariation("list", ctx, "x"));
    assertEquals("x", client.jsonVariation("missing", ctx, "x"));
    Evaluation<Object> missing = client.jsonVariationDetail("missing", ctx, "x");
    assertEquals(ErrorCode.FLAG_NOT_FOUND, missing.reason().errorCode());
    assertEquals(
        ErrorCode.INVALID_CONTEXT,
        client.boolVariationDetail("text", Map.of(), false).reason().errorCode());
  }

  @Test
  void aRaiseInTheBuildIsEvaluationFailedAndOneOnErrorCall() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    FakeClock clock = new FakeClock(false, false);
    ToggleSlashClient client = start(options().clock(clock));
    assertTrue(client.ready());
    errors.clear();
    clock.wallThrows = true;
    Evaluation<Boolean> detail = client.boolVariationDetail("f", Map.of("key", "u"), false);
    clock.wallThrows = false;
    assertFalse(detail.value());
    assertEquals(ErrorCode.EVALUATION_FAILED, detail.reason().errorCode());
    assertTrue(detail.variationIndex().isEmpty());
    assertEquals(1, errors.size());
    assertTrue(errors.get(0) instanceof IllegalStateException);
  }

  @Test
  void hooksSeeEveryCallAndARaisingHookChangesNothing() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 7)), "\"v1\"");
    List<EvaluationEvent> seen = new CopyOnWriteArrayList<>();
    ToggleSlashClient client =
        start(
            options()
                .afterEvaluation(event -> {
                  seen.add(event);
                  throw new IllegalArgumentException("hook");
                }));
    assertTrue(client.ready());
    Map<String, Object> ctx = Map.of("key", "u");
    assertTrue(client.boolVariation("f", ctx, false));
    assertEquals(false, client.boolVariation("nope", ctx, false));
    assertEquals(2, seen.size());
    EvaluationEvent first = seen.get(0);
    assertEquals("f", first.flagKey());
    assertEquals(ctx, first.context());
    assertEquals(true, first.value());
    assertEquals(0, first.variationIndex().getAsInt());
    assertEquals("\"v1\"", first.etag().orElseThrow());
    assertEquals(7L, first.flagVersion().getAsLong());
    assertEquals(ErrorCode.FLAG_NOT_FOUND, seen.get(1).reason().errorCode());
    assertTrue(seen.get(1).flagVersion().isEmpty());
    assertEquals(2, errors.stream().filter(e -> e instanceof IllegalArgumentException).count());
    client.close();
    Map<String, Object> body = body(server.events.get(0));
    List<?> sent = (List<?>) body.get("events");
    assertEquals(1, sent.size());
    assertEquals(1.0, ((Map<?, ?>) sent.get(0)).get("count"));
  }

  @Test
  void aThrowingOnErrorIsIgnored() {
    server.rulesetHandler = x -> FakeServer.reply(x, 500, "boom");
    ToggleSlashClient client =
        start(
            ClientOptions.builder(server.url(), FakeServer.KEY).stream(false)
                .onError(e -> {
                  throw new IllegalStateException("onError");
                }));
    assertFalse(client.ready());
    assertEquals("http_500", client.status().lastError().orElseThrow());
    assertEquals(false, client.boolVariation("f", Map.of("key", "u"), false));
  }

  @SuppressWarnings("unchecked")
  static Map<String, Object> body(FakeServer.Seen seen) {
    return (Map<String, Object>) Json.parse(seen.body());
  }

  @Test
  void usageGoesOutOnCloseAsExactlyTheFourFields() {
    server.serve(
        FakeServer.rulesetOf(FakeServer.flag("a", "true", 1), FakeServer.flag("b", "false", 1)),
        "\"v1\"");
    ToggleSlashClient client = start(options());
    assertTrue(client.ready());
    Map<String, Object> ctx = Map.of("key", "u");
    for (int i = 0; i < 3; i++) {
      client.boolVariation("a", ctx, false);
    }
    client.boolVariation("b", ctx, true);
    client.boolVariation("missing", ctx, true);
    client.close();
    assertEquals(1, server.events.size());
    FakeServer.Seen seen = server.events.get(0);
    assertEquals("POST", seen.method());
    assertEquals("application/json", seen.header("Content-Type"));
    Map<String, Object> body = body(seen);
    assertEquals(List.of("events"), List.copyOf(body.keySet()));
    List<?> entries = (List<?>) body.get("events");
    assertEquals(2, entries.size());
    for (Object entry : entries) {
      Map<?, ?> map = (Map<?, ?>) entry;
      assertEquals(4, map.size());
      assertTrue(map.containsKey("flag") && map.containsKey("variationIndex"));
      assertTrue(map.containsKey("count") && map.containsKey("day"));
      assertTrue(((String) map.get("day")).matches("\\d{4}-\\d{2}-\\d{2}"));
    }
    assertFalse(seen.text().contains("\"u\""));
    // nothing is counted after close
    client.boolVariation("a", ctx, false);
    assertEquals(1, server.events.size());
    assertEquals(StreamState.CLOSED, client.status().stream());
  }

  @Test
  void countsEitherSideOfUtcMidnightGoOutAsTwoDays() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("a", "true", 1)), "\"v1\"");
    FakeClock clock = new FakeClock(false, false);
    clock.wall.set(Instant.parse("2026-03-01T23:59:59.500Z").toEpochMilli());
    ToggleSlashClient client = start(options().clock(clock));
    assertTrue(client.ready());
    Map<String, Object> ctx = Map.of("key", "u");
    client.boolVariation("a", ctx, false);
    client.boolVariation("a", ctx, false);
    clock.wall.set(Instant.parse("2026-03-02T00:00:00.500Z").toEpochMilli());
    client.boolVariation("a", ctx, false);
    client.close();
    List<?> entries = (List<?>) body(server.events.get(0)).get("events");
    assertEquals(2, entries.size());
    for (Object entry : entries) {
      Map<?, ?> map = (Map<?, ?>) entry;
      if (map.get("day").equals("2026-03-01")) {
        assertEquals(2.0, map.get("count"));
      } else {
        assertEquals("2026-03-02", map.get("day"));
        assertEquals(1.0, map.get("count"));
      }
    }
  }

  @Test
  void moreThanFiveHundredEntriesGoAsSeveralRequests() {
    StringBuilder flags = new StringBuilder();
    String[] list = new String[1203];
    for (int i = 0; i < list.length; i++) {
      list[i] = FakeServer.flag("f" + i, "true", 1);
    }
    server.serve(FakeServer.rulesetOf(list), "\"v1\"");
    ToggleSlashClient client = start(options());
    assertTrue(client.ready());
    for (int i = 0; i < list.length; i++) {
      client.boolVariation("f" + i, Map.of("key", "u"), false);
    }
    client.close();
    assertEquals(3, server.events.size());
    int total = 0;
    for (FakeServer.Seen seen : server.events) {
      int n = ((List<?>) body(seen).get("events")).size();
      assertTrue(n <= 500);
      total += n;
    }
    assertEquals(list.length, total);
    assertNotNull(flags);
  }

  @Test
  void nothingPendingSendsNoRequest() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("a", "true", 1)), "\"v1\"");
    ToggleSlashClient client = start(options().flushInterval(Duration.ofMillis(20)));
    assertTrue(client.ready());
    try {
      Thread.sleep(150);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
    client.close();
    assertTrue(server.events.isEmpty());
  }

  @Test
  void aFailedFlushKeepsTheCountsAndTheNextOneSendsThem() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("a", "true", 1)), "\"v1\"");
    server.eventsHandler = x -> FakeServer.reply(x, 503, "later");
    ToggleSlashClient client = start(options().flushInterval(Duration.ofMillis(30)));
    assertTrue(client.ready());
    client.boolVariation("a", Map.of("key", "u"), false);
    waitFor("a failed flush", 5000, () -> server.events.size() >= 2);
    assertTrue(errors.stream().anyMatch(e -> "http_503".equals(((ToggleSlashException) e).code())));
    server.eventsHandler = x -> FakeServer.reply(x, 204, "");
    int before = server.events.size();
    waitFor("a good flush", 5000, () -> server.events.size() > before);
    Map<?, ?> entry = (Map<?, ?>) ((List<?>) body(server.events.get(before)).get("events")).get(0);
    assertEquals(1.0, entry.get("count"));
    int sent = server.events.size();
    try {
      Thread.sleep(150);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
    assertEquals(sent, server.events.size());
  }

  @Test
  void aClientErrorDropsTheBatchAndGoesOn() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("a", "true", 1)), "\"v1\"");
    server.eventsHandler = x -> FakeServer.reply(x, 400, "no");
    ToggleSlashClient client = start(options().flushInterval(Duration.ofMillis(30)));
    assertTrue(client.ready());
    client.boolVariation("a", Map.of("key", "u"), false);
    waitFor("one flush", 5000, () -> server.events.size() >= 1);
    try {
      Thread.sleep(200);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
    assertEquals(1, server.events.size());
  }

  @Test
  void aHeldRetryAfterMeansCloseLosesTheCountsAndSaysSo() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("a", "true", 1)), "\"v1\"");
    server.eventsHandler =
        x -> {
          x.getResponseHeaders().add("Retry-After", "30");
          FakeServer.reply(x, 429, "slow");
        };
    ToggleSlashClient client = start(options().flushInterval(Duration.ofMillis(30)));
    assertTrue(client.ready());
    Map<String, Object> ctx = Map.of("key", "u");
    client.boolVariation("a", ctx, false);
    waitFor("the 429", 5000, () -> server.events.size() >= 1);
    client.boolVariation("a", ctx, false);
    errors.clear();
    client.close();
    assertEquals(1, server.events.size());
    assertTrue(
        errors.stream()
            .anyMatch(e ->
                e.getMessage()
                    .equals("closed with 1 usage counts unsent: the server asked to wait")));
  }

  @Test
  void closeIsSafeTwiceAndEvaluationStillAnswers() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("a", "true", 1)), "\"v1\"");
    ToggleSlashClient client = start(options());
    assertTrue(client.ready());
    client.close();
    client.close();
    assertTrue(client.boolVariation("a", Map.of("key", "u"), false));
    assertEquals(StreamState.CLOSED, client.status().stream());
    assertTrue(client.ready());
  }

  @Test
  void closeBeforeTheFirstLoadSettlesReadyFalse() {
    CountDownLatch never = new CountDownLatch(1);
    server.rulesetHandler =
        x -> {
          try {
            never.await(10, TimeUnit.SECONDS);
          } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
          }
        };
    ToggleSlashClient client = start(options().requestTimeout(Duration.ofSeconds(30)));
    waitFor("the request", 5000, () -> !server.ruleset.isEmpty());
    long start = System.nanoTime();
    client.close();
    assertFalse(client.ready());
    assertTrue(System.nanoTime() - start < 3_000_000_000L);
    assertTrue(client.status().lastError().isEmpty());
    assertTrue(errors.isEmpty());
    never.countDown();
  }

  @Test
  void aRulesetThatNeverAnswersIsATimeoutAndReadyIsBounded() {
    server.rulesetHandler =
        x -> {
          try {
            Thread.sleep(3000);
          } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
          }
        };
    ToggleSlashClient client = start(options().requestTimeout(Duration.ofMillis(300)));
    long start = System.nanoTime();
    assertFalse(client.ready());
    assertTrue(System.nanoTime() - start < 2_000_000_000L);
    assertEquals("timeout", client.status().lastError().orElseThrow());
  }

  @Test
  void aBodyThatNeverEndsIsATimeoutToo() {
    server.rulesetHandler =
        x -> {
          x.sendResponseHeaders(200, 100_000);
          x.getResponseBody().write("{\"flags\":".getBytes());
          x.getResponseBody().flush();
          try {
            Thread.sleep(3000);
          } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
          }
        };
    ToggleSlashClient client = start(options().requestTimeout(Duration.ofMillis(300)));
    assertFalse(client.ready());
    assertEquals("timeout", client.status().lastError().orElseThrow());
  }

  @Test
  void aStreamChangeIsFetchedAndTheSameTagIsNot() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    ToggleSlashClient client =
        start(
            ClientOptions.builder(server.url(), FakeServer.KEY)
                .onError(errors::add)
                .requestTimeout(Duration.ofSeconds(5)));
    assertTrue(client.ready());
    waitFor("connected", 5000, () -> client.status().stream() == StreamState.CONNECTED);
    assertEquals("text/event-stream", server.stream.get(0).header("Accept"));
    int fetches = server.ruleset.size();
    server.push("event: changed\ndata: \"v1\"\n\n");
    server.push("event: other\ndata: x\n\n");
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "false", 2)), "\"v2\"");
    server.push("event: changed\ndata: \"v2\"\n\n");
    waitFor("the new ruleset", 5000, () -> "\"v2\"".equals(client.status().etag().orElse("")));
    assertFalse(client.boolVariation("f", Map.of("key", "u"), true));
    assertEquals(fetches + 1, server.ruleset.size());
    assertEquals("\"v1\"", server.ruleset.get(fetches).header("If-None-Match"));
  }

  @Test
  void aStreamThatEndsCleanLeavesLastErrorAloneAndIsRetried() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    ToggleSlashClient client =
        start(
            ClientOptions.builder(server.url(), FakeServer.KEY)
                .jitter(() -> 0.5)
                .onError(errors::add)
                .requestTimeout(Duration.ofSeconds(5)));
    assertTrue(client.ready());
    waitFor("connected", 5000, () -> client.status().stream() == StreamState.CONNECTED);
    server.push("END");
    waitFor("disconnected", 5000, () -> client.status().stream() != StreamState.CONNECTED);
    assertTrue(client.status().lastError().isEmpty());
    waitFor("a second stream", 8000, () -> server.stream.size() >= 2);
  }

  @Test
  void aNonOkStreamIsHttpStatusAndAPollFollows() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    server.streamHandler = x -> FakeServer.reply(x, 503, "no");
    ToggleSlashClient client =
        start(
            ClientOptions.builder(server.url(), FakeServer.KEY)
                .jitter(() -> 0.5)
                .onError(errors::add)
                .requestTimeout(Duration.ofSeconds(5)));
    assertTrue(client.ready());
    // one status() read is one snapshot; a second read could already see the next attempt
    waitFor(
        "http_503 while disconnected",
        5000,
        () -> {
          Status status = client.status();
          return "http_503".equals(status.lastError().orElse(""))
              && status.stream() == StreamState.DISCONNECTED;
        });
    waitFor("a poll after it", 8000, () -> server.ruleset.size() >= 2);
  }
}
