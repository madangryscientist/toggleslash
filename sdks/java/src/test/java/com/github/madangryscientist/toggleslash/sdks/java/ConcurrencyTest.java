package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.OutputStream;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import org.junit.jupiter.api.Test;

/** Threads, bounds, callbacks that re-enter the client, and the stream watchdog. */
class ConcurrencyTest {
  private static final Map<String, Object> USER = Map.of("key", "u1");

  private static Options.Builder options(FakeServer server) {
    return Options.builder(server.url(), "fs_server_test").stream(false)
        .pollInterval(Duration.ofSeconds(60))
        .flushInterval(Duration.ofSeconds(60));
  }

  private static void serve(FakeServer server) {
    server.on(
        "/sdk/v1/ruleset",
        x ->
            FakeServer.reply(
                x,
                200,
                FakeServer.ruleset(FakeServer.flag("b", 1, "false", "true")),
                "ETag",
                "\"v1\""));
    server.on("/sdk/v1/events", x -> FakeServer.reply(x, 200, "{}"));
  }

  @Test
  void manyThreadsEvaluateWhileCloseRacesThemAndAnotherClose() throws Exception {
    try (FakeServer server = new FakeServer()) {
      serve(server);
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      ToggleSlashClient client =
          ToggleSlashClient.create(options(server).onError(errors::add).build());
      assertTrue(client.ready());
      int workers = 16;
      AtomicBoolean stop = new AtomicBoolean();
      AtomicLong[] progress = new AtomicLong[workers];
      List<Throwable> thrown = new CopyOnWriteArrayList<>();
      Thread[] threads = new Thread[workers];
      for (int i = 0; i < workers; i++) {
        AtomicLong counter = new AtomicLong();
        progress[i] = counter;
        threads[i] =
            new Thread(() -> {
              try {
                while (!stop.get()) {
                  Detail<Boolean> d = client.boolVariationDetail("b", USER, false);
                  assertTrue(d.value() || d.variationIndex() == null || d.variationIndex() == 1);
                  client.stringVariation("b", USER, "x");
                  client.doubleVariation("b", USER, 0);
                  client.intVariationDetail("b", USER, 0);
                  client.variation("b", USER, null);
                  client.status();
                  client.ready();
                  counter.incrementAndGet();
                }
              } catch (Throwable t) {
                thrown.add(t);
              }
            });
        threads[i].start();
      }
      for (AtomicLong counter : progress) {
        assertTrue(Wait.until(5000, () -> counter.get() > 100));
      }
      Thread closerA = new Thread(client::close);
      Thread closerB = new Thread(client::close);
      closerA.start();
      closerB.start();
      closerA.join(10_000);
      closerB.join(10_000);
      assertFalse(closerA.isAlive());
      assertFalse(closerB.isAlive());
      long[] mark = new long[workers];
      for (int i = 0; i < workers; i++) {
        mark[i] = progress[i].get();
      }
      for (int i = 0; i < workers; i++) {
        int k = i;
        assertTrue(
            Wait.until(5000, () -> progress[k].get() > mark[k] + 50),
            "worker " + k + " stalled after close");
      }
      stop.set(true);
      for (Thread t : threads) {
        t.join(5000);
        assertFalse(t.isAlive());
      }
      client.close();
      assertEquals(List.of(), thrown);
      assertEquals(List.of(), errors);
    }
  }

  @Test
  void evaluationStaysFastWhileTheRulesetRequestAndTheUsageFlushHang() throws Exception {
    try (FakeServer server = new FakeServer()) {
      AtomicBoolean hangRuleset = new AtomicBoolean();
      server.on(
          "/sdk/v1/ruleset",
          x -> {
            if (hangRuleset.get()) {
              FakeServer.hang();
            }
            FakeServer.reply(
                x,
                200,
                FakeServer.ruleset(FakeServer.flag("b", 1, "false", "true")),
                "ETag",
                "\"v1\"");
          });
      server.on("/sdk/v1/events", x -> FakeServer.hang());
      ToggleSlashClient client =
          ToggleSlashClient.create(
              options(server)
                  .pollInterval(Duration.ofMillis(50))
                  .requestTimeout(Duration.ofMillis(300))
                  .build());
      assertTrue(client.ready());
      hangRuleset.set(true);
      client.boolVariation("b", USER, false);
      Thread flusher = new Thread(client::flushNow);
      flusher.setDaemon(true);
      flusher.start();
      assertTrue(Wait.until(5000, () -> !server.to("/sdk/v1/events").isEmpty()));
      Thread.sleep(150);
      long worst = 0;
      for (int i = 0; i < 2000; i++) {
        long t0 = System.nanoTime();
        assertTrue(client.boolVariation("b", USER, false));
        worst = Math.max(worst, System.nanoTime() - t0);
      }
      assertTrue(TimeUnit.NANOSECONDS.toMillis(worst) < 100, "slowest evaluation " + worst + " ns");
      long t0 = System.nanoTime();
      client.close();
      long took = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - t0);
      assertTrue(took < 1500, "close took " + took + " ms");
      flusher.join(5000);
    }
  }

  @Test
  void aHookSeesTheEtagAndTheFlagVersionOfOneRulesetAcrossASwap() throws Exception {
    try (FakeServer server = new FakeServer()) {
      AtomicInteger n = new AtomicInteger();
      server.on(
          "/sdk/v1/ruleset",
          x -> {
            boolean a = n.getAndIncrement() % 2 == 0;
            FakeServer.reply(
                x,
                200,
                FakeServer.ruleset(FakeServer.flag("b", a ? 1 : 2, "false", "true")),
                "ETag",
                a ? "\"a\"" : "\"b\"");
          });
      server.on("/sdk/v1/events", x -> FakeServer.reply(x, 200, "{}"));
      List<String> bad = new CopyOnWriteArrayList<>();
      AtomicInteger seenA = new AtomicInteger();
      AtomicInteger seenB = new AtomicInteger();
      ToggleSlashClient client =
          ToggleSlashClient.create(
              options(server)
                  .pollInterval(Duration.ofMillis(10))
                  .afterEvaluation(e -> {
                    boolean a = "\"a\"".equals(e.etag());
                    (a ? seenA : seenB).incrementAndGet();
                    long expected = a ? 1L : 2L;
                    if (e.flagVersion() == null || e.flagVersion() != expected) {
                      bad.add(e.etag() + " with version " + e.flagVersion());
                    }
                  })
                  .build());
      assertTrue(client.ready());
      long end = System.nanoTime() + TimeUnit.SECONDS.toNanos(2);
      while (System.nanoTime() < end && (seenA.get() < 5 || seenB.get() < 5)) {
        client.boolVariation("b", USER, false);
      }
      client.close();
      assertTrue(seenA.get() >= 5 && seenB.get() >= 5, "a=" + seenA + " b=" + seenB);
      assertEquals(List.of(), bad);
    }
  }

  @Test
  void callbacksMayCallBackIntoTheClient() throws Exception {
    try (FakeServer server = new FakeServer()) {
      serve(server);
      AtomicInteger depth = new AtomicInteger();
      AtomicInteger inner = new AtomicInteger();
      ToggleSlashClient[] holder = new ToggleSlashClient[1];
      holder[0] =
          ToggleSlashClient.create(
              options(server)
                  .afterEvaluation(e -> {
                    if (depth.getAndIncrement() == 0) {
                      holder[0].status();
                      holder[0].boolVariation("b", USER, false);
                      inner.incrementAndGet();
                    }
                    depth.decrementAndGet();
                  })
                  .build());
      assertTrue(holder[0].ready());
      assertTrue(holder[0].boolVariation("b", USER, false));
      assertEquals(1, inner.get());
      holder[0].close();
    }
    try (FakeServer server = new FakeServer()) {
      server.on("/sdk/v1/ruleset", x -> FakeServer.reply(x, 500, "no"));
      CountDownLatch called = new CountDownLatch(1);
      AtomicBoolean readyAnswer = new AtomicBoolean(true);
      ToggleSlashClient[] holder = new ToggleSlashClient[1];
      long t0 = System.nanoTime();
      holder[0] =
          ToggleSlashClient.create(
              options(server)
                  .onError(e -> {
                    readyAnswer.set(holder[0] == null || holder[0].ready());
                    if (holder[0] != null) {
                      holder[0].close();
                    }
                    called.countDown();
                  })
                  .build());
      assertTrue(called.await(10, TimeUnit.SECONDS));
      assertFalse(readyAnswer.get());
      holder[0].close();
      assertTrue(TimeUnit.NANOSECONDS.toSeconds(System.nanoTime() - t0) < 10);
    }
  }

  @Test
  void aQuietStreamEndsWithTimeoutWhenTheWatchdogExpires() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.on(
          "/sdk/v1/ruleset", x -> FakeServer.reply(x, 200, FakeServer.ruleset(), "ETag", "\"v1\""));
      server.on(
          "/sdk/v1/stream",
          x -> {
            OutputStream out = FakeServer.open(x);
            FakeServer.write(out, "event: connected\ndata: \"v1\"\n\n");
            out.flush();
            FakeServer.hang();
          });
      ToggleSlashClient client =
          ToggleSlashClient.create(
              options(server).stream(true)
                  .watchdog(Duration.ofMillis(400))
                  .jitter(() -> 1.0)
                  .build());
      assertTrue(client.ready());
      assertTrue(Wait.until(5000, () -> client.status().stream() == StreamState.CONNECTED));
      assertTrue(Wait.until(5000, () -> "timeout".equals(client.status().lastError())));
      assertTrue(Wait.until(5000, () -> client.status().stream() != StreamState.CONNECTED));
      client.close();
      assertEquals(StreamState.CLOSED, client.status().stream());
    }
  }

  @Test
  void keepAlivesRestartTheWatchdogAndDoNotMoveLastSyncAt() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.on(
          "/sdk/v1/ruleset", x -> FakeServer.reply(x, 200, FakeServer.ruleset(), "ETag", "\"v1\""));
      server.on(
          "/sdk/v1/stream",
          x -> {
            OutputStream out = FakeServer.open(x);
            FakeServer.write(out, "event: connected\ndata: \"v1\"\n\n");
            out.flush();
            for (int i = 0; i < 40; i++) {
              Thread.sleep(150);
              FakeServer.write(out, ": keep-alive\n\n");
              out.flush();
            }
          });
      ToggleSlashClient client =
          ToggleSlashClient.create(
              options(server).stream(true)
                  .watchdog(Duration.ofMillis(400))
                  .jitter(() -> 1.0)
                  .build());
      assertTrue(client.ready());
      assertTrue(Wait.until(5000, () -> client.status().stream() == StreamState.CONNECTED));
      java.time.Instant synced = client.status().lastSyncAt();
      Thread.sleep(1500);
      Status status = client.status();
      assertEquals(StreamState.CONNECTED, status.stream());
      assertEquals(null, status.lastError());
      assertTrue(synced.isBefore(status.lastSyncAt()) || synced.equals(status.lastSyncAt()));
      client.close();
    }
  }

  @Test
  void aCloseCausedFailureNeitherSetsLastErrorNorCallsOnError() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.on(
          "/sdk/v1/ruleset", x -> FakeServer.reply(x, 200, FakeServer.ruleset(), "ETag", "\"v1\""));
      server.on(
          "/sdk/v1/stream",
          x -> {
            OutputStream out = FakeServer.open(x);
            FakeServer.write(out, "event: connected\ndata: \"v1\"\n\n");
            out.flush();
            FakeServer.hang();
          });
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      ToggleSlashClient client =
          ToggleSlashClient.create(options(server).stream(true).onError(errors::add).build());
      assertTrue(client.ready());
      assertTrue(Wait.until(5000, () -> client.status().stream() == StreamState.CONNECTED));
      client.close();
      Thread.sleep(200);
      assertEquals(null, client.status().lastError());
      assertEquals(List.of(), errors);
    }
  }
}
