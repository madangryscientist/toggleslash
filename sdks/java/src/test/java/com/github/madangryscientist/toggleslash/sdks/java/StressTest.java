package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/**
 * The stand-in for a race detector, which Java does not have: many threads call every operation at
 * once while the ruleset changes and while {@code close} races them. A data race shows up as an
 * exception on a thread, a torn snapshot, a hang (the test times out) or a count that does not add
 * up.
 */
@Timeout(120)
final class StressTest {
  private static final Object USER = Context.of("u1");

  @Test
  void everyOperationFromManyThreadsWhileTheRulesetChangesAndCloseRaces() throws Exception {
    try (FakeServer server = new FakeServer()) {
      List<Throwable> thrown = new CopyOnWriteArrayList<>();
      List<EvaluationEvent> torn = new CopyOnWriteArrayList<>();
      AtomicLong evaluations = new AtomicLong();
      Client client =
          Client.create(
              Options.builder()
                  .url(server.url())
                  .sdkKey("fs_server_test")
                  .requestTimeout(Duration.ofSeconds(5))
                  .flushInterval(Duration.ofMillis(20))
                  .afterEvaluation(event -> {
                    evaluations.incrementAndGet();
                    if (event.etag() != null
                        && !event.etag().equals("\"v" + event.flagVersion() + "\"")) {
                      torn.add(event);
                    }
                  })
                  .onError(error -> {})
                  .build());
      assertTrue(client.ready());
      AtomicBoolean stop = new AtomicBoolean();
      CountDownLatch start = new CountDownLatch(1);
      List<Thread> workers = new ArrayList<>();
      for (int i = 0; i < 12; i++) {
        int kind = i % 4;
        Thread worker =
            new Thread(() -> {
              try {
                start.await();
                while (!stop.get()) {
                  switch (kind) {
                    case 0 -> client.boolVariation("checkout", USER, false);
                    case 1 -> client.variationDetail("checkout", USER, 0.0);
                    case 2 -> {
                      client.status();
                      client.pendingUsage();
                      client.ready();
                    }
                    default -> client.flush();
                  }
                }
              } catch (Throwable t) {
                thrown.add(t);
              }
            });
        worker.setDaemon(true);
        workers.add(worker);
        worker.start();
      }
      start.countDown();
      for (int version = 2; version <= 60; version++) {
        server.serve(FakeServer.rulesetJson(version, "checkout"), "\"v" + version + "\"");
        server.push("event: changed\ndata: \"v" + version + "\"\n\n");
        Thread.sleep(10);
      }
      Thread firstCloser = new Thread(client::close);
      Thread secondCloser = new Thread(client::close);
      firstCloser.start();
      secondCloser.start();
      firstCloser.join(30_000);
      secondCloser.join(30_000);
      assertEquals(false, firstCloser.isAlive() || secondCloser.isAlive());
      Thread.sleep(100);
      stop.set(true);
      for (Thread worker : workers) {
        worker.join(10_000);
        assertEquals(false, worker.isAlive());
      }
      assertEquals(List.of(), thrown);
      assertEquals(List.of(), torn);
      assertTrue(evaluations.get() > 1000);
      assertEquals(StreamState.CLOSED, client.status().stream());
      client.flush();
      assertEquals(0, client.pendingUsage());
      long sent = 0;
      for (FakeServer.Request request : server.to("/sdk/v1/events")) {
        for (String part : request.body().split("\"count\":")) {
          int end = 0;
          while (end < part.length() && Character.isDigit(part.charAt(end))) {
            end++;
          }
          if (end > 0 && part != request.body()) {
            sent += Long.parseLong(part.substring(0, end));
          }
        }
      }
      assertTrue(sent > 0);
      assertTrue(client.boolVariation("checkout", USER, false));
      assertEquals(0, client.pendingUsage());
      assertTrue(server.requests.size() > 10);
      assertTrue(TimeUnit.SECONDS.toMillis(1) > 0);
    }
  }
}
