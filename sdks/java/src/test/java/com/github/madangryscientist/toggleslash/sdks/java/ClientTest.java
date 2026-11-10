package com.github.madangryscientist.toggleslash.sdks.java;

import static com.github.madangryscientist.toggleslash.sdks.java.FakeServer.flag;
import static com.github.madangryscientist.toggleslash.sdks.java.FakeServer.ruleset;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTimeoutPreemptively;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ReasonKind;
import com.sun.net.httpserver.HttpExchange;
import java.io.File;
import java.io.IOException;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.time.ZoneId;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.TreeMap;
import java.util.TreeSet;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;

/**
 * What the conformance harness cannot reach: threads, hung requests, the watchdog on a real socket,
 * the hold cap on every route, the UTC day split, a failing evaluator, and the headers on the wire.
 * The server is a real loopback socket; time is faked only through the options' seams.
 */
class ClientTest {
  private static final Duration LIMIT = Duration.ofSeconds(60);
  private static final Context USER = Context.user("u1");

  private static Options.Builder options(FakeServer server) {
    return Options.builder(server.url(), "fs_server_test").stream(false);
  }

  private static void waitFor(java.util.function.BooleanSupplier condition, String what)
      throws InterruptedException {
    long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
    while (!condition.getAsBoolean()) {
      if (System.nanoTime() > deadline) {
        throw new AssertionError("did not happen within 10 s: " + what);
      }
      Thread.sleep(5);
    }
  }

  // ---- threads ------------------------------------------------------------------------------

  /** Java has no race detector; many threads and a close in the middle stand in for one. */
  @Test
  void everyOperationFromManyThreadsWithCloseRacingAndTwoClosesRacing() throws Exception {
    assertTimeoutPreemptively(
        LIMIT,
        () -> {
          try (FakeServer server =
              new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"").events(200)) {
            List<Throwable> errors = Collections.synchronizedList(new ArrayList<>());
            Client client =
                Client.create(
                    options(server)
                        .flushInterval(Duration.ofMillis(20))
                        .pollInterval(Duration.ofMillis(20))
                        .onError(errors::add)
                        .build());
            assertTrue(client.ready());
            ExecutorService pool = Executors.newFixedThreadPool(18);
            List<Future<?>> work = new ArrayList<>();
            for (int w = 0; w < 14; w++) {
              final int id = w;
              work.add(
                  pool.submit(
                      () -> {
                        for (int i = 0; i < 3000; i++) {
                          Context who = Context.user("u" + id + "-" + i);
                          client.boolVariation("f", who, false);
                          assertNotNull(client.stringVariationDetail("f", who, "x").reason());
                          assertNotNull(client.status());
                          client.ready();
                        }
                      }));
            }
            CountDownLatch go = new CountDownLatch(1);
            for (int c = 0; c < 4; c++) {
              work.add(
                  pool.submit(
                      () -> {
                        go.await();
                        client.close();
                        return null;
                      }));
            }
            Thread.sleep(50);
            go.countDown();
            for (Future<?> future : work) {
              future.get();
            }
            pool.shutdown();
            assertEquals(StreamState.CLOSED, client.status().stream());
            assertEquals(List.of(), errors);
          }
        });
  }

  @Test
  void variationReturnsQuicklyWhileARulesetRequestAndAUsageFlushHang() throws Exception {
    assertTimeoutPreemptively(
        LIMIT,
        () -> {
          try (FakeServer server = new FakeServer()) {
            AtomicInteger rulesets = new AtomicInteger();
            server
                .on(
                    "/sdk/v1/ruleset",
                    x -> {
                      if (rulesets.getAndIncrement() == 0) {
                        x.getResponseHeaders().set("ETag", "\"v1\"");
                        FakeServer.send(x, 200, ruleset(flag("f", "true", 1)));
                      } else {
                        server.hang(x);
                      }
                    })
                .on("/sdk/v1/events", server::hang);
            Client client =
                Client.create(
                    options(server)
                        .pollInterval(Duration.ofMillis(20))
                        .flushInterval(Duration.ofMillis(20))
                        .requestTimeout(Duration.ofSeconds(2))
                        .build());
            assertTrue(client.ready());
            client.boolVariation("f", USER, false);
            waitFor(() -> server.requests("/sdk/v1/ruleset").size() >= 2, "a hung ruleset request");
            waitFor(() -> !server.requests("/sdk/v1/events").isEmpty(), "a hung usage flush");
            long start = System.nanoTime();
            for (int i = 0; i < 2000; i++) {
              assertEquals(true, client.boolVariationDetail("f", USER, false).value());
              client.status();
            }
            long millis = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - start);
            assertTrue(millis < 1000, "4000 calls took " + millis + " ms while requests hung");
            client.close();
          }
        });
  }

  @Test
  void aHooksEtagAndFlagVersionBelongToTheSameRulesetUnderConcurrentSwaps() throws Exception {
    assertTimeoutPreemptively(
        LIMIT,
        () -> {
          try (FakeServer server = new FakeServer().events(200)) {
            AtomicInteger n = new AtomicInteger();
            server.on(
                "/sdk/v1/ruleset",
                x -> {
                  int which = n.getAndIncrement() % 2;
                  x.getResponseHeaders().set("ETag", "\"e" + which + "\"");
                  FakeServer.send(x, 200, ruleset(flag("f", "true", which + 1)));
                });
            Set<String> pairs = Collections.synchronizedSet(new HashSet<>());
            Client client =
                Client.create(
                    options(server)
                        .pollInterval(Duration.ofMillis(5))
                        .afterEvaluation(
                            event ->
                                pairs.add(
                                    event.etag().orElse("none")
                                        + "/"
                                        + event.flagVersion().orElse(-1.0)))
                        .build());
            assertTrue(client.ready());
            ExecutorService pool = Executors.newFixedThreadPool(4);
            List<Future<?>> work = new ArrayList<>();
            for (int t = 0; t < 4; t++) {
              work.add(
                  pool.submit(
                      () -> {
                        long end = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(1500);
                        while (System.nanoTime() < end) {
                          client.boolVariation("f", USER, false);
                        }
                      }));
            }
            for (Future<?> future : work) {
              future.get();
            }
            pool.shutdown();
            client.close();
            assertEquals(Set.of("\"e0\"/1.0", "\"e1\"/2.0"), new HashSet<>(pairs));
          }
        });
  }

  @Test
  void aHookAndAnOnErrorThatCallBackIntoTheClientDoNotDeadlock() throws Exception {
    assertTimeoutPreemptively(
        LIMIT,
        () -> {
          try (FakeServer server = new FakeServer()) {
            AtomicInteger asked = new AtomicInteger();
            server.on(
                "/sdk/v1/ruleset",
                x -> {
                  if (asked.getAndIncrement() == 0) {
                    x.getResponseHeaders().set("ETag", "\"v1\"");
                    FakeServer.send(x, 200, ruleset(flag("f", "true", 1)));
                  } else {
                    FakeServer.send(x, 500, "");
                  }
                });
            AtomicReference<Client> self = new AtomicReference<>();
            CountDownLatch errored = new CountDownLatch(1);
            CountDownLatch hooked = new CountDownLatch(1);
            ThreadLocal<Boolean> inside = new ThreadLocal<>();
            Client client =
                Client.create(
                    options(server)
                        .pollInterval(Duration.ofMillis(20))
                        .afterEvaluation(
                            event -> {
                              if (inside.get() == null) {
                                inside.set(true);
                                try {
                                  self.get().status();
                                  self.get().boolVariation("f", USER, false);
                                  hooked.countDown();
                                } finally {
                                  inside.remove();
                                }
                              }
                            })
                        .onError(
                            error -> {
                              try {
                                self.get().status();
                                self.get().boolVariation("f", USER, false);
                                errored.countDown();
                              } catch (RuntimeException e) {
                                // self is not set yet: the next error will be
                              }
                            })
                        .build());
            self.set(client);
            assertTrue(client.ready());
            client.boolVariation("f", USER, false);
            assertTrue(hooked.await(10, TimeUnit.SECONDS), "the hook called back and returned");
            assertTrue(errored.await(10, TimeUnit.SECONDS), "onError called back and returned");
            client.close();
          }
        });
  }

  // ---- time ---------------------------------------------------------------------------------

  private static String streamBody(String etag) {
    return "event: connected\ndata: " + etag + "\n\n";
  }

  private static void eventStream(HttpExchange x, String first) throws IOException {
    x.getResponseHeaders().set("Content-Type", "text/event-stream");
    x.sendResponseHeaders(200, 0);
    OutputStream out = x.getResponseBody();
    out.write(first.getBytes(StandardCharsets.UTF_8));
    out.flush();
  }

  /** A real loopback socket that goes quiet: the seam only shortens the watchdog's length. */
  @Test
  void aStreamWithNoBytesIsDroppedAsTimeout() throws Exception {
    assertTimeoutPreemptively(
        LIMIT,
        () -> {
          try (FakeServer server =
              new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"")) {
            server.on(
                "/sdk/v1/stream",
                x -> {
                  eventStream(x, streamBody("\"v1\""));
                  server.hang(x);
                });
            Client client =
                Client.create(
                    Options.builder(server.url(), "fs_server_test")
                        .pollInterval(Duration.ofMillis(100))
                        .streamIdle(Duration.ofMillis(400))
                        .build());
            assertTrue(client.ready());
            waitFor(() -> client.status().stream() == StreamState.CONNECTED, "connected");
            long start = System.nanoTime();
            waitFor(() -> client.status().lastError().equals(Optional.of("timeout")), "timeout");
            long millis = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - start);
            assertTrue(millis >= 250, "dropped after " + millis + " ms, not after the quiet");
            client.close();
          }
        });
  }

  @Test
  void aKeepAliveRestartsTheWatchdog() throws Exception {
    assertTimeoutPreemptively(
        LIMIT,
        () -> {
          try (FakeServer server =
              new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"")) {
            server.on(
                "/sdk/v1/stream",
                x -> {
                  eventStream(x, streamBody("\"v1\""));
                  for (int i = 0; i < 12; i++) {
                    Thread.sleep(100);
                    x.getResponseBody().write(": keep-alive\n\n".getBytes(StandardCharsets.UTF_8));
                    x.getResponseBody().flush();
                  }
                  server.hang(x);
                });
            Client client =
                Client.create(
                    Options.builder(server.url(), "fs_server_test")
                        .pollInterval(Duration.ofMillis(100))
                        .streamIdle(Duration.ofMillis(400))
                        .build());
            assertTrue(client.ready());
            waitFor(() -> client.status().stream() == StreamState.CONNECTED, "connected");
            Thread.sleep(1000);
            Status status = client.status();
            assertEquals(StreamState.CONNECTED, status.stream());
            assertEquals(Optional.empty(), status.lastError());
            client.close();
          }
        });
  }

  @Test
  void aRetryAfterAbove300IsHeldFor300SecondsOnTheRulesetRoute() throws Exception {
    assertTimeoutPreemptively(
        LIMIT,
        () -> {
          try (FakeServer server = new FakeServer()) {
            server.on("/sdk/v1/ruleset", x -> limited(x));
            Client client = Client.create(options(server).nanoTime(() -> 1_000_000L).build());
            assertFalse(client.ready());
            assertEquals(Duration.ofSeconds(300), client.syncHoldRemaining());
            client.close();
          }
        });
  }

  @Test
  void aRetryAfterAbove300IsHeldFor300SecondsOnTheStreamRoute() throws Exception {
    assertTimeoutPreemptively(
        LIMIT,
        () -> {
          try (FakeServer server =
              new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"")) {
            server.on("/sdk/v1/stream", x -> limited(x));
            Client client =
                Client.create(
                    Options.builder(server.url(), "fs_server_test")
                        .nanoTime(() -> 1_000_000L)
                        .build());
            assertTrue(client.ready());
            waitFor(() -> client.syncHoldRemaining().compareTo(Duration.ZERO) > 0, "a hold");
            assertEquals(Duration.ofSeconds(300), client.syncHoldRemaining());
            client.close();
          }
        });
  }

  @Test
  void aRetryAfterAbove300IsHeldFor300SecondsOnTheEventsRoute() throws Exception {
    assertTimeoutPreemptively(
        LIMIT,
        () -> {
          try (FakeServer server =
              new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"")) {
            server.on("/sdk/v1/events", x -> limited(x));
            List<String> errors = Collections.synchronizedList(new ArrayList<>());
            Client client =
                Client.create(
                    options(server)
                        .nanoTime(() -> 1_000_000L)
                        .onError(e -> errors.add(String.valueOf(e.getMessage())))
                        .build());
            assertTrue(client.ready());
            client.boolVariation("f", USER, false);
            client.flushNow();
            assertEquals(Duration.ofSeconds(300), client.usageHoldRemaining());
            assertEquals(Duration.ZERO, client.syncHoldRemaining());
            long start = System.nanoTime();
            client.close();
            assertTrue(
                System.nanoTime() - start < TimeUnit.SECONDS.toNanos(5),
                "close waited out the hold");
            assertTrue(
                errors.stream().anyMatch(m -> m.contains("1 usage counts unsent")),
                "the counts lost to the hold were reported: " + errors);
          }
        });
  }

  private static void limited(HttpExchange x) throws IOException {
    x.getResponseHeaders().set("Retry-After", "99999");
    FakeServer.send(x, 429, "");
  }

  /** A clock a test moves. */
  private static final class MovingClock extends Clock {
    private volatile Instant now;

    MovingClock(Instant now) {
      this.now = now;
    }

    @Override
    public ZoneId getZone() {
      return ZoneOffset.UTC;
    }

    @Override
    public Clock withZone(ZoneId zone) {
      return this;
    }

    @Override
    public Instant instant() {
      return now;
    }
  }

  @Test
  void countsRecordedEitherSideOfUtcMidnightGoOutAsTwoDays() throws Exception {
    try (FakeServer server =
        new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"").events(200)) {
      MovingClock clock = new MovingClock(Instant.parse("2026-03-31T23:59:59Z"));
      Client client = Client.create(options(server).clock(clock).build());
      assertTrue(client.ready());
      client.boolVariation("f", USER, false);
      clock.now = Instant.parse("2026-04-01T00:00:01Z");
      client.boolVariation("f", USER, false);
      client.boolVariation("f", Context.user("u2"), false);
      client.flushNow();
      List<FakeServer.Seen> sent = server.requests("/sdk/v1/events");
      assertEquals(1, sent.size());
      Map<String, Double> byDay = new TreeMap<>();
      Object events = ((Map<?, ?>) Json.parse(sent.get(0).body())).get("events");
      for (Object entry : (List<?>) events) {
        Map<?, ?> row = (Map<?, ?>) entry;
        byDay.merge((String) row.get("day"), (Double) row.get("count"), Double::sum);
      }
      assertEquals(Map.of("2026-03-31", 1.0, "2026-04-01", 2.0), byDay);
      client.close();
    }
  }

  // ---- values -------------------------------------------------------------------------------

  @Test
  void anEvaluationThatRaisesGivesTheFallbackEvaluationFailedAndOneOnError() throws Exception {
    try (FakeServer server =
        new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"").events(200)) {
      List<Throwable> errors = Collections.synchronizedList(new ArrayList<>());
      Client client =
          Client.create(
              options(server)
                  .evaluator(
                      (rules, key, context) -> {
                        throw new IllegalStateException("boom");
                      })
                  .onError(errors::add)
                  .build());
      assertTrue(client.ready());
      Detail<Boolean> detail = client.boolVariationDetail("f", USER, true);
      assertEquals(true, detail.value());
      assertTrue(detail.variationIndex().isEmpty());
      assertEquals(ReasonKind.ERROR, detail.reason().kind());
      assertEquals(Optional.of(ErrorCode.EVALUATION_FAILED), detail.reason().code());
      assertEquals(1, errors.size());
      client.flushNow();
      assertEquals(0, server.requests("/sdk/v1/events").size(), "nothing was counted");
      client.close();
    }
  }

  @Test
  void theIntegerAccessorTakesWholeNumbersInSigned64BitsAndNothingElse() throws Exception {
    try (FakeServer server =
        new FakeServer()
            .ruleset(
                ruleset(
                    flag("huge", "9223372036854775808", 1),
                    flag("hugeNegative", "-9223372036854777856", 1),
                    flag("min", "-9223372036854775808", 1),
                    flag("fraction", "1.5", 1),
                    flag("two", "2.0", 1)),
                "\"v1\"")) {
      Client client = Client.create(options(server).build());
      assertTrue(client.ready());
      for (String key : List.of("huge", "hugeNegative", "fraction")) {
        Detail<Long> detail = client.intVariationDetail(key, USER, 7);
        assertEquals(7L, detail.value(), key);
        assertEquals(Optional.of(ErrorCode.TYPE_MISMATCH), detail.reason().code(), key);
      }
      assertEquals(Long.MIN_VALUE, client.intVariation("min", USER, 7));
      assertEquals(2L, client.intVariation("two", USER, 7));
      client.close();
    }
  }

  // ---- process and wire ---------------------------------------------------------------------

  @Test
  void theSdksThreadsAreDaemonThreads() throws Exception {
    try (FakeServer server = new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"")) {
      Client client = Client.create(options(server).build());
      assertTrue(client.ready());
      List<Thread> ours = new ArrayList<>();
      for (Thread thread : Thread.getAllStackTraces().keySet()) {
        if (thread.getName().startsWith("toggleslash-") && thread.isAlive()) {
          ours.add(thread);
        }
      }
      assertEquals(2, ours.size(), ours.toString());
      for (Thread thread : ours) {
        assertTrue(thread.isDaemon(), thread.getName());
      }
      client.close();
    }
  }

  @Test
  void aClientThatIsNeverClosedDoesNotKeepTheProcessFromExiting() throws Exception {
    try (FakeServer server = new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"")) {
      String classes =
          new File(Client.class.getProtectionDomain().getCodeSource().getLocation().toURI())
                  .getPath()
              + File.pathSeparator
              + new File(
                      ClientTest.class.getProtectionDomain().getCodeSource().getLocation().toURI())
                  .getPath();
      String java = ProcessHandle.current().info().command().orElse("java");
      Process child =
          new ProcessBuilder(java, "-cp", classes, UnclosedMain.class.getName(), server.url())
              .redirectErrorStream(true)
              .start();
      boolean exited = child.waitFor(30, TimeUnit.SECONDS);
      String output = new String(child.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
      if (!exited) {
        child.destroyForcibly();
      }
      assertTrue(exited, "the child JVM kept running: " + output);
      assertEquals("returned", output.trim());
    }
  }

  /** The headers the SDK sets, and the three the JDK's HTTP client adds on its own. */
  @Test
  void everyRequestCarriesOnlyTheProtocolsHeadersAndTheJdksThree() throws Exception {
    try (FakeServer server =
        new FakeServer().ruleset(ruleset(flag("f", "true", 1)), "\"v1\"").events(200)) {
      server.on(
          "/sdk/v1/stream",
          x -> {
            eventStream(x, streamBody("\"v1\""));
            server.hang(x);
          });
      Client polling = Client.create(options(server).pollInterval(Duration.ofMillis(30)).build());
      Client streaming = Client.create(Options.builder(server.url(), "fs_server_test").build());
      assertTrue(polling.ready());
      assertTrue(streaming.ready());
      polling.boolVariation("f", Context.user("someone@example.com"), false);
      polling.flushNow();
      waitFor(
          () -> server.requests("/sdk/v1/ruleset").size() >= 3, "a conditional ruleset request");
      waitFor(() -> !server.requests("/sdk/v1/stream").isEmpty(), "a stream request");
      waitFor(() -> !server.requests("/sdk/v1/events").isEmpty(), "an events request");
      Set<String> names = new TreeSet<>();
      for (FakeServer.Seen request : server.all()) {
        for (Map.Entry<String, List<String>> header : request.headers().entrySet()) {
          names.add(header.getKey().toLowerCase(java.util.Locale.ROOT));
          if (header.getKey().equalsIgnoreCase("User-Agent")) {
            assertTrue(
                header.getValue().get(0).matches("Java-http-client/[0-9][0-9.]*[A-Za-z0-9.+-]*"),
                "User-Agent names the library only: " + header.getValue());
          }
        }
        assertFalse(request.body().contains("someone@example.com"));
        assertFalse(request.path().contains("someone"));
      }
      Set<String> allowed =
          Set.of(
              "authorization",
              "accept",
              "if-none-match",
              "content-type",
              "host",
              "user-agent",
              "content-length");
      assertTrue(allowed.containsAll(names), "headers on the wire: " + names);
      System.out.println("request headers seen: " + names);
      assertTrue(
          server.requests("/sdk/v1/ruleset").stream()
              .anyMatch(
                  r ->
                      r.headers().keySet().stream()
                          .anyMatch(k -> k.equalsIgnoreCase("If-None-Match"))));
      polling.close();
      streaming.close();
    }
  }
}
