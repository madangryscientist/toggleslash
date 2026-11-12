package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;

/**
 * Java has no race detector, so these stand in for one: many threads call every operation at once,
 * {@code close} races them, two {@code close} calls race each other, and every wait is bounded.
 */
class ConcurrencyTest {
  private static final Map<String, Object> CONTEXT = Map.of("kind", "user", "key", "u1");
  private static final long BOUND_MILLIS = 15_000;

  /** Serves a new version of the ruleset on every request, its ETag naming the flag's version. */
  private static FakeServer changing(AtomicInteger version) throws Exception {
    FakeServer server = new FakeServer();
    server.handler((request, conn) -> {
      if (request.path().equals("/sdk/v1/ruleset")) {
        int n = version.incrementAndGet();
        conn.respond(200, Support.ruleset(Support.flag("f", "true", n)), "ETag", "\"v" + n + "\"");
      } else if (request.path().equals("/sdk/v1/events")) {
        conn.respond(204, "");
      } else {
        conn.respond(404, "");
      }
    });
    return server;
  }

  @Test
  void everyOperationFromManyThreadsWithCloseRacingAndTwoClosesRacing() throws Exception {
    List<Throwable> errors = new CopyOnWriteArrayList<>();
    AtomicInteger version = new AtomicInteger();
    try (FakeServer server = changing(version)) {
      Client client =
          Client.create(
              Support.options(server).stream(false)
                  .pollInterval(Duration.ofMillis(5))
                  .flushInterval(Duration.ofMillis(5))
                  .requestTimeout(Duration.ofSeconds(5))
                  .onError(errors::add)
                  .afterEvaluation(event -> client0(event))
                  .build());
      assertTrue(client.ready());
      int workers = 16;
      AtomicBoolean stop = new AtomicBoolean();
      AtomicInteger[] progress = new AtomicInteger[workers];
      List<Throwable> raised = new CopyOnWriteArrayList<>();
      CountDownLatch started = new CountDownLatch(workers);
      List<Thread> threads = new ArrayList<>();
      for (int i = 0; i < workers; i++) {
        AtomicInteger count = new AtomicInteger();
        progress[i] = count;
        Thread thread =
            new Thread(() -> {
              started.countDown();
              try {
                while (!stop.get()) {
                  client.boolVariation("f", CONTEXT, false);
                  client.variationDetail("f", CONTEXT, null);
                  client.intVariation("f", CONTEXT, 0);
                  client.stringVariation("f", CONTEXT, "");
                  client.floatVariation("f", CONTEXT, 0);
                  client.status();
                  client.ready();
                  client.flush();
                  count.incrementAndGet();
                }
              } catch (Throwable e) {
                raised.add(e);
              }
            });
        threads.add(thread);
        thread.start();
      }
      assertTrue(started.await(5, TimeUnit.SECONDS));
      waitForProgress(progress, 20);
      int[] before = new int[workers];
      for (int i = 0; i < workers; i++) {
        before[i] = progress[i].get();
      }
      // Two closes race each other, while the workers keep calling.
      CountDownLatch go = new CountDownLatch(1);
      List<Long> waits = new CopyOnWriteArrayList<>();
      List<Thread> closers = new ArrayList<>();
      for (int i = 0; i < 2; i++) {
        Thread closer =
            new Thread(() -> {
              try {
                go.await();
                long start = System.nanoTime();
                client.close();
                waits.add(Duration.ofNanos(System.nanoTime() - start).toMillis());
              } catch (Throwable e) {
                raised.add(e);
              }
            });
        closers.add(closer);
        closer.start();
      }
      go.countDown();
      for (Thread closer : closers) {
        closer.join(BOUND_MILLIS);
        assertFalse(closer.isAlive(), "close did not return within the bound");
      }
      // Workers made progress during the race and go on being answered after the close.
      waitForProgress(progress, 20);
      stop.set(true);
      for (Thread thread : threads) {
        thread.join(BOUND_MILLIS);
        assertFalse(thread.isAlive());
      }
      assertEquals(List.of(), raised);
      assertEquals(2, waits.size());
      for (int i = 0; i < workers; i++) {
        assertTrue(progress[i].get() > before[i], "worker " + i + " made no progress");
      }
      assertEquals("closed", client.status().stream().value());
    }
  }

  private static void client0(EvaluationEvent event) {
    // The hook runs on the caller's thread; it only needs to be called.
    assertNotNull(event.flagKey());
  }

  private static void waitForProgress(AtomicInteger[] progress, int rounds) throws Exception {
    int[] start = new int[progress.length];
    for (int i = 0; i < progress.length; i++) {
      start[i] = progress[i].get();
    }
    Support.await(
        "every worker to make progress",
        BOUND_MILLIS,
        () -> {
          for (int i = 0; i < progress.length; i++) {
            if (progress[i].get() < start[i] + rounds) {
              return false;
            }
          }
          return true;
        });
  }

  @Test
  void aHooksEtagAndFlagVersionBelongToTheSameRulesetUnderConcurrentSwaps() throws Exception {
    AtomicInteger version = new AtomicInteger();
    List<String> mismatches = new CopyOnWriteArrayList<>();
    AtomicInteger seen = new AtomicInteger();
    try (FakeServer server = changing(version);
        Client client =
            Client.create(
                Support.options(server).stream(false)
                    .pollInterval(Duration.ofMillis(2))
                    .afterEvaluation(event -> {
                      seen.incrementAndGet();
                      String expected = "\"v" + event.flagVersion() + "\"";
                      if (!expected.equals(event.etag())) {
                        mismatches.add(event.etag() + " with version " + event.flagVersion());
                      }
                    })
                    .build())) {
      assertTrue(client.ready());
      long end = System.nanoTime() + Duration.ofSeconds(1).toNanos();
      List<Thread> threads = new ArrayList<>();
      for (int i = 0; i < 4; i++) {
        Thread thread =
            new Thread(() -> {
              while (System.nanoTime() < end) {
                client.boolVariation("f", CONTEXT, false);
              }
            });
        threads.add(thread);
        thread.start();
      }
      for (Thread thread : threads) {
        thread.join();
      }
      assertEquals(List.of(), mismatches);
      assertTrue(version.get() > 10, "the ruleset swapped " + version.get() + " times");
      assertTrue(seen.get() > 1000);
    }
  }

  @Test
  void callbacksThatCallBackIntoTheClientDoNotDeadlock() throws Exception {
    AtomicReference<Client> holder = new AtomicReference<>();
    CountDownLatch holderSet = new CountDownLatch(1);
    AtomicInteger hookCalls = new AtomicInteger();
    List<Long> readyWaits = new CopyOnWriteArrayList<>();
    CountDownLatch errored = new CountDownLatch(1);
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> conn.respond(500, "no"));
      Client client =
          Client.create(
              Support.options(server).stream(false)
                  .afterEvaluation(event -> {
                    if (hookCalls.incrementAndGet() == 1) {
                      holder.get().status();
                      holder.get().variation("f", CONTEXT, null);
                    }
                  })
                  .onError(e -> {
                    try {
                      holderSet.await();
                    } catch (InterruptedException x) {
                      Thread.currentThread().interrupt();
                      return;
                    }
                    Client c = holder.get();
                    long start = System.nanoTime();
                    boolean ready = c.ready();
                    readyWaits.add(Duration.ofNanos(System.nanoTime() - start).toMillis());
                    c.status();
                    c.variation("f", CONTEXT, null);
                    if (!ready) {
                      errored.countDown();
                    }
                  })
                  .build());
      holder.set(client);
      holderSet.countDown();
      // The hook calls back into the client once; its inner variation runs the hook again.
      client.variation("f", CONTEXT, null);
      assertEquals(2, hookCalls.get());
      assertTrue(errored.await(10, TimeUnit.SECONDS), "onError never ran on the sync thread");
      assertFalse(client.ready());
      client.close();
      for (long wait : readyWaits) {
        assertTrue(wait < 1000, "ready on the SDK's own thread waited " + wait + " ms");
      }
    }
  }

  @Test
  void readyAndCloseFromTheSdksOwnThreadDoNotWait() throws Exception {
    AtomicReference<Client> holder = new AtomicReference<>();
    CountDownLatch holderSet = new CountDownLatch(1);
    List<Long> readyWaits = new CopyOnWriteArrayList<>();
    List<Long> closeWaits = new CopyOnWriteArrayList<>();
    CountDownLatch called = new CountDownLatch(1);
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> conn.respond(500, "no"));
      Client client =
          Client.create(
              Support.options(server).stream(false)
                  .onError(e -> {
                    try {
                      holderSet.await();
                    } catch (InterruptedException x) {
                      Thread.currentThread().interrupt();
                      return;
                    }
                    Client c = holder.get();
                    long start = System.nanoTime();
                    c.ready();
                    readyWaits.add(Duration.ofNanos(System.nanoTime() - start).toMillis());
                    start = System.nanoTime();
                    c.close();
                    closeWaits.add(Duration.ofNanos(System.nanoTime() - start).toMillis());
                    called.countDown();
                  })
                  .build());
      holder.set(client);
      holderSet.countDown();
      assertTrue(called.await(10, TimeUnit.SECONDS));
      assertTrue(readyWaits.get(0) < 1000);
      assertTrue(closeWaits.get(0) < 1000);
      // The shutdown it started finishes, and a second close from outside waits for it.
      client.close();
      assertEquals("closed", client.status().stream().value());
    }
  }

  @Test
  void variationReturnsAtOnceWhileARulesetRequestHangs() throws Exception {
    AtomicInteger calls = new AtomicInteger();
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> {
        if (calls.incrementAndGet() == 1) {
          conn.respond(200, Support.boolFlags(), "ETag", "\"v1\"");
        } else {
          conn.awaitClientGone();
        }
      });
      Client client =
          Client.create(
              Support.options(server).stream(false)
                  .pollInterval(Duration.ofMillis(20))
                  .requestTimeout(Duration.ofSeconds(2))
                  .build());
      assertTrue(client.ready());
      Support.await("the hanging request", 5000, () -> calls.get() >= 2);
      long start = System.nanoTime();
      for (int i = 0; i < 1000; i++) {
        assertTrue(client.boolVariation("f", CONTEXT, false));
        client.variationDetail("f", CONTEXT, null);
        client.status();
      }
      assertTrue(Duration.ofNanos(System.nanoTime() - start).toMillis() < 1000);
      long closing = System.nanoTime();
      client.close();
      assertTrue(Duration.ofNanos(System.nanoTime() - closing).toSeconds() < 5);
    }
  }

  @Test
  void firstLoadThatNeverAnswersSettlesReadyAfterTheTimeout() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> conn.awaitClientGone());
      Client client =
          Client.create(
              Support.options(server).stream(false).requestTimeout(Duration.ofMillis(500)).build());
      long start = System.nanoTime();
      assertFalse(client.ready());
      long took = Duration.ofNanos(System.nanoTime() - start).toMillis();
      assertTrue(took >= 400 && took < 4000, "ready took " + took + " ms");
      assertEquals("timeout", client.status().lastError());
      client.close();
    }
  }

  @Test
  void variationReturnsAtOnceWhileAUsageFlushHangs() throws Exception {
    CountDownLatch flushing = new CountDownLatch(1);
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> {
        if (request.path().equals("/sdk/v1/events")) {
          flushing.countDown();
          conn.awaitClientGone();
        } else {
          conn.respond(200, Support.boolFlags(), "ETag", "\"v1\"");
        }
      });
      Client client =
          Client.create(
              Support.options(server).stream(false).requestTimeout(Duration.ofSeconds(2)).build());
      assertTrue(client.ready());
      assertTrue(client.boolVariation("f", CONTEXT, false));
      Thread flusher = new Thread(client::flush);
      flusher.setDaemon(true);
      flusher.start();
      assertTrue(flushing.await(5, TimeUnit.SECONDS));
      long start = System.nanoTime();
      for (int i = 0; i < 1000; i++) {
        assertTrue(client.boolVariation("f", CONTEXT, false));
        client.status();
      }
      assertTrue(Duration.ofNanos(System.nanoTime() - start).toMillis() < 1000);
      long closing = System.nanoTime();
      client.close();
      long took = Duration.ofNanos(System.nanoTime() - closing).toMillis();
      // About twice requestTimeout at most.
      assertTrue(took < 8000, "close took " + took + " ms");
      flusher.join(8000);
      assertFalse(flusher.isAlive());
    }
  }
}
