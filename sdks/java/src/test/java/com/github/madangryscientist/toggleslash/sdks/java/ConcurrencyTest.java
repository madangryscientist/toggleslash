package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/**
 * Every operation from many threads at once, {@code close} racing {@code variation}, and two {@code
 * close} calls racing. Java has no race detector in the JDK, so the stand-in is a stress test: 16
 * threads, every operation, a bound on every wait, and a check that each worker kept making
 * progress after {@code close} began.
 */
@Timeout(120)
final class ConcurrencyTest {
  private static final Map<String, Object> CTX = Map.of("kind", "user", "key", "u1");
  private static final int WORKERS = 16;

  private FakeServer server;

  @BeforeEach
  void start() {
    server = new FakeServer();
    server.ruleset =
        FakeServer.serving(
            "\"v1\"",
            Support.ruleset(
                Support.boolFlag("a", true, 1),
                Support.boolFlag("b", false, 1),
                Support.flag("n", "[1,2,3]", 2, "[]", 1)));
    server.stream = server.sse("event: connected\ndata: \"v1\"\n\n");
  }

  @AfterEach
  void stop() {
    server.close();
  }

  @Test
  void everyOperationFromManyThreadsWithCloseRacingVariationAndTwoClosesRacing() throws Exception {
    List<Throwable> errors = Collections.synchronizedList(new ArrayList<>());
    List<Throwable> workerFailures = Collections.synchronizedList(new ArrayList<>());
    List<EvaluationEvent> hooked = Collections.synchronizedList(new ArrayList<>());
    Client client =
        Client.create(
            Support.options(server)
                .pollInterval(Duration.ofMillis(20))
                .flushInterval(Duration.ofMillis(20))
                .requestTimeout(Duration.ofSeconds(3))
                .afterEvaluation(event -> {
                  if (hooked.size() < 1000) {
                    hooked.add(event);
                  }
                })
                .onError(errors::add)
                .build());
    assertTrue(client.ready());

    AtomicBoolean stop = new AtomicBoolean();
    AtomicLong[] progress = new AtomicLong[WORKERS];
    Thread[] workers = new Thread[WORKERS];
    for (int i = 0; i < WORKERS; i++) {
      AtomicLong count = new AtomicLong();
      progress[i] = count;
      int id = i;
      workers[i] =
          new Thread(
              () -> {
                try {
                  while (!stop.get()) {
                    Map<String, Object> context =
                        Map.of("kind", "user", "key", "u" + id + count.get());
                    switch ((int) (count.get() % 9)) {
                      case 0:
                        client.booleanVariation("a", context, false);
                        break;
                      case 1:
                        client.stringVariationDetail("a", context, "x");
                        break;
                      case 2:
                        client.longVariation("n", context, 0);
                        break;
                      case 3:
                        client.doubleVariationDetail("n", context, 0);
                        break;
                      case 4:
                        client.jsonVariation("b", context, null);
                        break;
                      case 5:
                        client.variation("a", context, true);
                        break;
                      case 6:
                        client.status();
                        break;
                      case 7:
                        client.flush();
                        break;
                      default:
                        client.ready();
                        break;
                    }
                    count.incrementAndGet();
                  }
                } catch (Throwable t) {
                  workerFailures.add(t);
                }
              },
              "worker-" + i);
      workers[i].setDaemon(true);
      workers[i].start();
    }

    waitForProgress(progress, snapshot(progress), 90);

    long[] atClose = snapshot(progress);
    CountDownLatch go = new CountDownLatch(1);
    CountDownLatch closed = new CountDownLatch(2);
    for (int i = 0; i < 2; i++) {
      Thread closer =
          new Thread(() -> {
            try {
              go.await();
              client.close();
            } catch (Throwable t) {
              workerFailures.add(t);
            } finally {
              closed.countDown();
            }
          });
      closer.setDaemon(true);
      closer.start();
    }
    go.countDown();
    assertTrue(closed.await(15, TimeUnit.SECONDS), "both closes returned");

    // Evaluation still answers after close: every worker keeps making progress.
    waitForProgress(progress, atClose, 90);
    stop.set(true);
    for (Thread worker : workers) {
      worker.join(10_000);
      assertFalse(worker.isAlive(), worker.getName() + " finished");
    }
    assertEquals(List.of(), workerFailures);
    assertEquals(Status.class, client.status().getClass());
    assertEquals(StreamState.CLOSED, client.status().stream());
    assertFalse(hooked.isEmpty());
  }

  private static long[] snapshot(AtomicLong[] progress) {
    long[] values = new long[progress.length];
    for (int i = 0; i < values.length; i++) {
      values[i] = progress[i].get();
    }
    return values;
  }

  /** Waits until every worker has made at least {@code more} operations past the snapshot. */
  private static void waitForProgress(AtomicLong[] progress, long[] from, long more) {
    Support.until(
        "every worker made progress",
        30_000,
        () -> {
          for (int i = 0; i < progress.length; i++) {
            if (progress[i].get() < from[i] + more) {
              return false;
            }
          }
          return true;
        });
  }
}
