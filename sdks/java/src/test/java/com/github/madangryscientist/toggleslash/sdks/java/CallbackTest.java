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
import java.util.concurrent.atomic.AtomicLong;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** A callback that calls back into the client does not deadlock. */
@Timeout(60)
final class CallbackTest {
  private static final Map<String, Object> CTX = Map.of("kind", "user", "key", "u1");

  private FakeServer server;

  @BeforeEach
  void start() {
    server = new FakeServer();
  }

  @AfterEach
  void stop() {
    server.close();
  }

  @Test
  void aHookThatCallsStatusAndVariationDoesNotDeadlock() throws Exception {
    server.ruleset =
        FakeServer.serving("\"v1\"", Support.ruleset(Support.boolFlag("checkout", true, 1)));
    ThreadLocal<Boolean> inside = new ThreadLocal<>();
    List<String> seen = Collections.synchronizedList(new ArrayList<>());
    Client[] holder = new Client[1];
    holder[0] =
        Client.create(
            Support.options(server).stream(false)
                .afterEvaluation(event -> {
                  if (inside.get() != null) {
                    return;
                  }
                  inside.set(Boolean.TRUE);
                  try {
                    seen.add(String.valueOf(holder[0].status().etag()));
                    seen.add(String.valueOf(holder[0].booleanVariation("checkout", CTX, false)));
                    seen.add(String.valueOf(holder[0].ready()));
                  } finally {
                    inside.remove();
                  }
                })
                .build());
    assertTrue(holder[0].ready());
    Thread caller =
        new Thread(() -> assertTrue(holder[0].booleanVariation("checkout", CTX, false)));
    caller.start();
    caller.join(10_000);
    assertFalse(caller.isAlive(), "the call returned");
    assertEquals(List.of("\"v1\"", "true", "true"), seen);
    holder[0].close();
  }

  @Test
  void anOnErrorOnTheSyncThreadCanCallStatusVariationAndReadyWithoutWaiting() throws Exception {
    server.ruleset = (exchange, request) -> FakeServer.reply(exchange, 500, "");
    CountDownLatch done = new CountDownLatch(1);
    AtomicLong elapsed = new AtomicLong();
    List<String> threads = Collections.synchronizedList(new ArrayList<>());
    Client[] holder = new Client[1];
    holder[0] =
        Client.create(
            Support.options(server).stream(false)
                .pollInterval(Duration.ofSeconds(30))
                .requestTimeout(Duration.ofSeconds(5))
                .onError(error -> {
                  if (done.getCount() == 0) {
                    return;
                  }
                  threads.add(Thread.currentThread().getName());
                  long start = System.nanoTime();
                  Client client = holder[0];
                  while (client == null) {
                    Thread.onSpinWait();
                    client = holder[0];
                  }
                  client.status();
                  client.booleanVariation("checkout", CTX, false);
                  client.ready();
                  elapsed.set((System.nanoTime() - start) / 1_000_000L);
                  done.countDown();
                })
                .build());
    assertTrue(done.await(10, TimeUnit.SECONDS), "onError ran to the end");
    assertEquals("toggleslash-sync", threads.get(0));
    assertTrue(elapsed.get() < 1000, "the calls took " + elapsed.get() + " ms");
    holder[0].close();
  }

  @Test
  void aCloseFromAnOnErrorOnTheFlushThreadReturnsAtOnce() throws Exception {
    server.ruleset =
        FakeServer.serving("\"v1\"", Support.ruleset(Support.boolFlag("checkout", true, 1)));
    server.events = (exchange, request) -> FakeServer.reply(exchange, 500, "");
    CountDownLatch done = new CountDownLatch(1);
    AtomicLong elapsed = new AtomicLong();
    List<String> threads = Collections.synchronizedList(new ArrayList<>());
    Client[] holder = new Client[1];
    holder[0] =
        Client.create(
            Support.options(server).stream(false)
                .flushInterval(Duration.ofMillis(30))
                .requestTimeout(Duration.ofSeconds(5))
                .onError(error -> {
                  Client client = holder[0];
                  if (client == null || done.getCount() == 0) {
                    return;
                  }
                  threads.add(Thread.currentThread().getName());
                  long start = System.nanoTime();
                  client.ready();
                  client.close();
                  elapsed.set((System.nanoTime() - start) / 1_000_000L);
                  done.countDown();
                })
                .build());
    assertTrue(holder[0].ready());
    holder[0].booleanVariation("checkout", CTX, false);
    assertTrue(done.await(10, TimeUnit.SECONDS), "onError ran to the end");
    assertEquals("toggleslash-flush", threads.get(0));
    assertTrue(elapsed.get() < 1000, "ready and close took " + elapsed.get() + " ms");
    Support.until(
        "the close finished", 5000, () -> holder[0].status().stream() == StreamState.CLOSED);
    holder[0].close();
  }
}
