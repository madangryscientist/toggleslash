package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTimeoutPreemptively;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

/**
 * Java has no race detector, so these stand in for one: many threads call into one client while it
 * is closed from two threads at once, and every wait has a bound so a deadlock fails the test.
 */
class ConcurrencyTest {
  @Test
  void closeRacesVariationAndAnotherClose() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route("/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.ruleset(1, 1)));
      server.route("/sdk/v1/events", (req, ex) -> FakeServer.reply(ex, 200, Map.of(), "{}"));
      for (int round = 0; round < 15; round++) {
        assertTimeoutPreemptively(Duration.ofSeconds(20), () -> oneRound(server));
      }
    }
  }

  private static void oneRound(FakeServer server) throws Exception {
    Client client =
        Client.create(
            Options.builder(server.url(), "k").stream(false)
                .pollInterval(Duration.ofMillis(2))
                .flushInterval(Duration.ofMillis(3))
                .requestTimeout(Duration.ofSeconds(5))
                .build());
    client.ready();
    List<Throwable> raised = new CopyOnWriteArrayList<>();
    AtomicBoolean closed = new AtomicBoolean();
    AtomicInteger answers = new AtomicInteger();
    Thread[] callers = new Thread[16];
    CountDownLatch go = new CountDownLatch(1);
    for (int i = 0; i < callers.length; i++) {
      callers[i] =
          new Thread(() -> {
            try {
              go.await();
              while (!closed.get()) {
                Object answer = client.variation("f", Map.of("key", "u"), Boolean.FALSE);
                client.status();
                // Before the first load it is the fallback, after it the served value, and a
                // close does not take the held ruleset away.
                if (answer != Boolean.TRUE && answer != Boolean.FALSE) {
                  raised.add(new AssertionError("odd answer " + answer));
                }
                answers.incrementAndGet();
              }
            } catch (Throwable t) {
              raised.add(t);
            }
          });
      callers[i].start();
    }
    go.countDown();
    Thread.sleep(40);
    Thread closerA = new Thread(client::close);
    Thread closerB = new Thread(client::close);
    closerA.start();
    closerB.start();
    closerA.join();
    closerB.join();
    closed.set(true);
    for (Thread caller : callers) {
      caller.join();
    }
    assertEquals(List.of(), raised);
    assertTrue(answers.get() > 0);
    assertEquals(StreamState.CLOSED, client.status().stream());
    assertEquals(Boolean.TRUE, client.variation("f", Map.of("key", "u"), Boolean.FALSE));
    // A third close, long after the others, returns at once.
    client.close();
  }
}
