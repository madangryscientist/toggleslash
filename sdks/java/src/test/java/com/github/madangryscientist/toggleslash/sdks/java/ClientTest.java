package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/**
 * The client against a scripted loopback server: loading, the result order, hooks, errors and
 * close.
 */
@Timeout(60)
final class ClientTest {
  private static final Object USER = Context.of("u1");

  private static Options.Builder options(FakeServer server) {
    return Options.builder()
        .url(server.url())
        .sdkKey("fs_server_test")
        .requestTimeout(Duration.ofSeconds(5))
        .pollInterval(Duration.ofSeconds(5))
        .flushInterval(Duration.ofHours(1));
  }

  @Test
  void loadsTheRulesetAndServesIt() {
    try (FakeServer server = new FakeServer();
        Client client = Client.create(options(server).build())) {
      assertTrue(client.ready());
      assertTrue(client.boolVariation("checkout", USER, false));
      Detail<Boolean> detail = client.boolVariationDetail("checkout", USER, false);
      assertEquals(1, detail.variationIndex());
      assertEquals(ReasonKind.FALLTHROUGH, detail.reason().kind());
      Status status = client.status();
      assertEquals("\"v1\"", status.etag());
      assertNotNull(status.lastSyncAt());
      assertNull(status.lastError());
    }
  }

  @Test
  void answersTheFallbackBeforeTheRulesetLoadsAndWhileTheRequestHangs() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset = (exchange, request) -> server.hang();
      try (Client client = Client.create(options(server).build())) {
        assertTrue(Eventually.holds(5000, () -> !server.to("/sdk/v1/ruleset").isEmpty()));
        long start = System.nanoTime();
        Detail<Boolean> detail = client.boolVariationDetail("checkout", USER, false);
        assertTrue(Duration.ofNanos(System.nanoTime() - start).toMillis() < 500);
        assertFalse(detail.value());
        assertNull(detail.variationIndex());
        assertEquals(ErrorCode.NOT_READY, detail.reason().code());
      }
    }
  }

  @Test
  void readyIsFalseAfterTheRequestTimeoutAndStatusSaysWhy() {
    try (FakeServer server = new FakeServer()) {
      server.ruleset = (exchange, request) -> server.hang();
      try (Client client =
          Client.create(options(server).requestTimeout(Duration.ofMillis(300)).build())) {
        long start = System.nanoTime();
        assertFalse(client.ready());
        assertTrue(Duration.ofNanos(System.nanoTime() - start).toSeconds() < 4);
        assertEquals(Status.TIMEOUT, client.status().lastError());
      }
    }
  }

  @Test
  void variationReturnsWhileTheFlushHangs() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.events = (exchange, request) -> server.hang();
      Client client = Client.create(options(server).requestTimeout(Duration.ofSeconds(2)).build());
      try {
        assertTrue(client.ready());
        client.boolVariation("checkout", USER, false);
        Thread flusher = new Thread(client::flush);
        flusher.setDaemon(true);
        flusher.start();
        assertTrue(Eventually.holds(5000, () -> !server.to("/sdk/v1/events").isEmpty()));
        long start = System.nanoTime();
        assertTrue(client.boolVariation("checkout", USER, false));
        assertTrue(Duration.ofNanos(System.nanoTime() - start).toMillis() < 500);
        flusher.join(10_000);
      } finally {
        client.close();
      }
    }
  }

  @Test
  void anUnknownFlagIsAnErrorAndCountsNothing() {
    try (FakeServer server = new FakeServer();
        Client client = Client.create(options(server).build())) {
      assertTrue(client.ready());
      Detail<Boolean> detail = client.boolVariationDetail("missing", USER, true);
      assertTrue(detail.value());
      assertEquals(ErrorCode.FLAG_NOT_FOUND, detail.reason().code());
      assertEquals(0, client.pendingUsage());
    }
  }

  @Test
  void aWrongKindIsATypeMismatchAndCountsNothing() {
    try (FakeServer server = new FakeServer();
        Client client = Client.create(options(server).build())) {
      assertTrue(client.ready());
      Detail<String> detail = client.stringVariationDetail("checkout", USER, "fb");
      assertEquals("fb", detail.value());
      assertNull(detail.variationIndex());
      assertEquals(ErrorCode.TYPE_MISMATCH, detail.reason().code());
      assertEquals(0, client.pendingUsage());
      assertTrue(client.boolVariation("checkout", USER, false));
      assertEquals(1, client.pendingUsage());
    }
  }

  @Test
  void anIntegerAccessorAcceptsTwoPointZeroAndRefusesAFraction() {
    try (FakeServer server = new FakeServer()) {
      server.serve(
          "{\"flags\":["
              + FakeServer.flag("whole", "[0,2.0]", 1, 1)
              + ","
              + FakeServer.flag("half", "[0,2.5]", 1, 1)
              + ","
              + FakeServer.flag("huge", "[0,9223372036854775808]", 1, 1)
              + ","
              + FakeServer.flag("edge", "[0,-9223372036854775808]", 1, 1)
              + "],\"segments\":[]}",
          "\"v2\"");
      try (Client client = Client.create(options(server).build())) {
        assertTrue(client.ready());
        assertEquals(2L, client.intVariation("whole", USER, -1));
        assertEquals(-1L, client.intVariation("half", USER, -1));
        assertEquals(
            ErrorCode.TYPE_MISMATCH, client.intVariationDetail("half", USER, -1).reason().code());
        assertEquals(
            ErrorCode.TYPE_MISMATCH, client.intVariationDetail("huge", USER, -1).reason().code());
        assertEquals(Long.MIN_VALUE, client.intVariation("edge", USER, 0));
        assertEquals(2.5, client.numberVariation("half", USER, 0));
      }
    }
  }

  @Test
  void aThrowingEvaluationIsEvaluationFailedWithOneOnError() {
    try (FakeServer server = new FakeServer()) {
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Options.Builder builder =
          options(server)
              .onError(errors::add)
              .evaluation((ruleset, flagKey, context) -> {
                throw new IllegalStateException("boom");
              });
      try (Client client = Client.create(builder.build())) {
        assertTrue(client.ready());
        Detail<Boolean> detail = client.boolVariationDetail("checkout", USER, false);
        assertFalse(detail.value());
        assertNull(detail.variationIndex());
        assertEquals(ErrorCode.EVALUATION_FAILED, detail.reason().code());
        assertEquals(1, errors.stream().filter(e -> "boom".equals(e.getMessage())).count());
        assertEquals(0, client.pendingUsage());
      }
    }
  }

  @Test
  void aThrowingHookAndAThrowingOnErrorNeverReachTheCaller() {
    try (FakeServer server = new FakeServer()) {
      AtomicInteger seen = new AtomicInteger();
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Options.Builder builder =
          options(server)
              .afterEvaluation(event -> {
                seen.incrementAndGet();
                throw new IllegalArgumentException("hook");
              })
              .onError(error -> {
                errors.add(error);
                throw new IllegalStateException("onError");
              });
      try (Client client = Client.create(builder.build())) {
        assertTrue(client.ready());
        assertTrue(client.boolVariation("checkout", USER, false));
        assertEquals(1, seen.get());
        assertTrue(errors.stream().anyMatch(e -> "hook".equals(e.getMessage())));
      }
    }
  }

  @Test
  void aHookMayCallBackIntoTheClient() {
    try (FakeServer server = new FakeServer()) {
      List<EvaluationEvent> events = new CopyOnWriteArrayList<>();
      Client[] holder = new Client[1];
      AtomicInteger depth = new AtomicInteger();
      Options.Builder builder =
          options(server)
              .afterEvaluation(event -> {
                events.add(event);
                if (depth.getAndIncrement() == 0) {
                  holder[0].boolVariation("checkout", USER, false);
                  holder[0].status();
                  holder[0].flush();
                  assertTrue(holder[0].ready());
                }
              });
      try (Client client = Client.create(builder.build())) {
        holder[0] = client;
        assertTrue(client.ready());
        assertTrue(client.boolVariation("checkout", USER, false));
        assertEquals(2, events.size());
        assertEquals("\"v1\"", events.get(0).etag());
        assertEquals(1L, events.get(0).flagVersion());
        assertEquals(true, events.get(0).value());
        assertEquals("checkout", events.get(0).flagKey());
      }
    }
  }

  @Test
  void onErrorOnTheSyncThreadMayCallReadyAndClose() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset = (exchange, request) -> FakeServer.send(exchange, 500, Map.of(), "no");
      java.util.concurrent.CompletableFuture<Client> holder =
          new java.util.concurrent.CompletableFuture<>();
      java.util.concurrent.CompletableFuture<String> outcome =
          new java.util.concurrent.CompletableFuture<>();
      Options.Builder builder =
          options(server)
              .onError(error -> {
                if (outcome.isDone()) {
                  return;
                }
                try {
                  Client client = holder.get();
                  boolean ready = client.ready();
                  client.close();
                  outcome.complete("ready=" + ready);
                } catch (Exception e) {
                  outcome.completeExceptionally(e);
                }
              });
      Client client = Client.create(builder.build());
      holder.complete(client);
      assertEquals("ready=false", outcome.get(10, java.util.concurrent.TimeUnit.SECONDS));
      client.close();
      assertEquals(StreamState.CLOSED, client.status().stream());
    }
  }

  @Test
  void closeTwiceAndEvaluationAfterCloseAnswerAndCountNothing() {
    try (FakeServer server = new FakeServer()) {
      Client client = Client.create(options(server).build());
      assertTrue(client.ready());
      assertTrue(client.boolVariation("checkout", USER, false));
      client.close();
      client.close();
      assertEquals(StreamState.CLOSED, client.status().stream());
      assertEquals(0, client.pendingUsage());
      assertTrue(client.boolVariation("checkout", USER, false));
      assertEquals(0, client.pendingUsage());
      assertEquals(1, server.to("/sdk/v1/events").size());
    }
  }

  @Test
  void closeBeforeTheFirstLoadFinishesMakesReadyFalse() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset = (exchange, request) -> server.hang();
      Client client = Client.create(options(server).build());
      assertTrue(Eventually.holds(5000, () -> !server.to("/sdk/v1/ruleset").isEmpty()));
      long start = System.nanoTime();
      client.close();
      assertTrue(Duration.ofNanos(System.nanoTime() - start).toSeconds() < 4);
      assertFalse(client.ready());
    }
  }

  @Test
  void theSdkThreadsAreDaemonThreads() {
    try (FakeServer server = new FakeServer();
        Client client = Client.create(options(server).build())) {
      assertTrue(client.ready());
      int found = 0;
      for (Thread thread : Thread.getAllStackTraces().keySet()) {
        if (thread.getName().equals("toggleslash-sync")
            || thread.getName().equals("toggleslash-flush")) {
          assertTrue(thread.isDaemon(), thread.getName());
          found++;
        }
      }
      assertTrue(found >= 2);
    }
  }

  @Test
  void aRetryAfterOfMoreThanThreeHundredSecondsHoldsThreeHundredOnTheRulesetRoute() {
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          (exchange, request) ->
              FakeServer.send(exchange, 429, Map.of("Retry-After", "301"), "slow down");
      try (Client client = Client.create(options(server).stream(false).build())) {
        assertFalse(client.ready());
        assertEquals("http_429", client.status().lastError());
        assertHeldAboutThreeHundredSeconds(client.rulesetHoldNanos());
        assertEquals(0, client.usageHoldNanos());
        assertEquals(1, server.to("/sdk/v1/ruleset").size());
      }
    }
  }

  @Test
  void aRetryAfterOfMoreThanThreeHundredSecondsHoldsThreeHundredOnTheStreamRoute() {
    try (FakeServer server = new FakeServer()) {
      server.stream =
          (exchange, request) ->
              FakeServer.send(exchange, 429, Map.of("Retry-After", "86400"), "slow down");
      try (Client client = Client.create(options(server).build())) {
        assertTrue(client.ready());
        assertTrue(Eventually.holds(5000, () -> client.rulesetHoldNanos() > 0));
        assertHeldAboutThreeHundredSeconds(client.rulesetHoldNanos());
        assertEquals("http_429", client.status().lastError());
      }
    }
  }

  @Test
  void aRetryAfterOfMoreThanThreeHundredSecondsHoldsThreeHundredOnTheEventsRoute() {
    try (FakeServer server = new FakeServer()) {
      server.events =
          (exchange, request) ->
              FakeServer.send(exchange, 429, Map.of("Retry-After", "301"), "slow down");
      Client client = Client.create(options(server).build());
      try {
        assertTrue(client.ready());
        client.boolVariation("checkout", USER, false);
        client.flush();
        assertHeldAboutThreeHundredSeconds(client.usageHoldNanos());
        assertEquals(1, client.pendingUsage());
        client.flush();
        assertEquals(1, server.to("/sdk/v1/events").size());
        assertEquals(0, client.rulesetHoldNanos());
      } finally {
        client.close();
      }
    }
  }

  @Test
  void aRetryAfterThatIsNotDecimalDigitsHoldsNothing() {
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          (exchange, request) ->
              FakeServer.send(exchange, 429, Map.of("Retry-After", "1e2"), "slow down");
      try (Client client = Client.create(options(server).stream(false).build())) {
        assertFalse(client.ready());
        assertEquals(0, client.rulesetHoldNanos());
      }
    }
  }

  private static void assertHeldAboutThreeHundredSeconds(long nanos) {
    long seconds = Duration.ofNanos(nanos).toSeconds();
    assertTrue(seconds >= 290 && seconds <= 300, "held " + seconds + " s");
  }
}
