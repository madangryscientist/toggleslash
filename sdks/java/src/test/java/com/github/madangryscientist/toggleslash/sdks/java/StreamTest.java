package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.internal.Clock;
import com.github.madangryscientist.toggleslash.sdks.java.internal.Tuning;
import com.github.madangryscientist.toggleslash.sdks.java.testkit.Fake;
import com.sun.net.httpserver.HttpExchange;
import java.io.IOException;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** The stream, its watchdog, and the clock seam. */
@Timeout(60)
class StreamTest {
  private static final String BOOLS = "[false,true]";

  private static OutputStream open(HttpExchange x) throws IOException {
    x.getResponseHeaders().set("Content-Type", "text/event-stream");
    x.sendResponseHeaders(200, 0);
    return x.getResponseBody();
  }

  private static void send(OutputStream out, String text) throws IOException {
    out.write(text.getBytes(StandardCharsets.UTF_8));
    out.flush();
  }

  private static boolean until(long millis, BooleanSupplier condition) throws InterruptedException {
    long end = System.nanoTime() + millis * 1_000_000L;
    while (System.nanoTime() < end) {
      if (condition.getAsBoolean()) {
        return true;
      }
      Thread.sleep(5);
    }
    return condition.getAsBoolean();
  }

  private static Client open(Fake fake, Tuning tuning, long pollMillis) {
    return Client.create(
        Options.builder()
            .url(fake.url())
            .sdkKey("k")
            .pollInterval(Duration.ofMillis(pollMillis))
            .requestTimeout(Duration.ofSeconds(3))
            .flushInterval(Duration.ofMinutes(5))
            .build(),
        tuning);
  }

  @Test
  void aQuietStreamTripsTheWatchdogAndReconnects() throws Exception {
    Fake fake = new Fake();
    fake.ruleset = x -> Fake.rulesetReply(x, "\"v1\"", Fake.body(Fake.flag("on", 1, BOOLS, 1)));
    CountDownLatch end = new CountDownLatch(1);
    fake.stream =
        x -> {
          OutputStream out = open(x);
          send(out, "event: connected\ndata: \"v1\"\n\n");
          end.await();
          x.close();
        };
    Tuning tuning = new Tuning();
    tuning.watchdog = Duration.ofMillis(300);
    try (fake;
        Client client = open(fake, tuning, 5000)) {
      assertTrue(client.ready());
      assertTrue(until(3000, () -> client.status().stream() == StreamState.connected));
      assertTrue(
          until(3000, () -> "timeout".equals(client.status().lastError())),
          "status " + client.status());
      assertEquals(StreamState.disconnected, client.status().stream());
      assertTrue(until(8000, () -> fake.count("/sdk/v1/stream") >= 2), "no reconnect");
      end.countDown();
    }
  }

  @Test
  void keepAliveCommentsHoldTheWatchdogOffWithoutBreakingConnection() throws Exception {
    Fake fake = new Fake();
    fake.ruleset = x -> Fake.rulesetReply(x, "\"v1\"", Fake.body(Fake.flag("on", 1, BOOLS, 1)));
    CountDownLatch end = new CountDownLatch(1);
    fake.stream =
        x -> {
          OutputStream out = open(x);
          send(out, "event: connected\ndata: \"v1\"\n\n");
          while (!end.await(100, java.util.concurrent.TimeUnit.MILLISECONDS)) {
            send(out, ": keep-alive\n\n");
          }
          x.close();
        };
    Tuning tuning = new Tuning();
    tuning.watchdog = Duration.ofMillis(400);
    try (fake;
        Client client = open(fake, tuning, 5000)) {
      assertTrue(client.ready());
      assertTrue(until(3000, () -> client.status().stream() == StreamState.connected));
      Thread.sleep(1600);
      assertEquals(StreamState.connected, client.status().stream());
      assertEquals(null, client.status().lastError());
      assertEquals(1, fake.count("/sdk/v1/stream"));
      end.countDown();
    }
  }

  @Test
  void aChangedEventFetchesTheNewRuleset() throws Exception {
    AtomicInteger version = new AtomicInteger(1);
    Fake fake = new Fake();
    fake.ruleset =
        x -> {
          int v = version.get();
          Fake.rulesetReply(
              x, "\"v" + v + "\"", Fake.body(Fake.flag("count", v, "[1,7]", v == 1 ? 0 : 1)));
        };
    CountDownLatch flip = new CountDownLatch(1);
    CountDownLatch end = new CountDownLatch(1);
    fake.stream =
        x -> {
          OutputStream out = open(x);
          send(out, "event: connected\ndata: \"v1\"\n\n");
          flip.await();
          version.set(2);
          send(out, "event: changed\ndata: \"v2\"\n\n");
          end.await();
          x.close();
        };
    try (fake;
        Client client = open(fake, new Tuning(), 60_000)) {
      assertTrue(client.ready());
      assertEquals(1L, client.intVariation("count", Context.of("u"), 0));
      assertTrue(until(3000, () -> client.status().stream() == StreamState.connected));
      flip.countDown();
      assertTrue(until(5000, () -> client.intVariation("count", Context.of("u"), 0) == 7L));
      assertEquals("\"v2\"", client.status().etag());
      end.countDown();
    }
  }

  @Test
  void aStatusReadNeverShowsAFailureBesideConnected() throws Exception {
    AtomicInteger attempt = new AtomicInteger();
    Fake fake = new Fake();
    fake.ruleset = x -> Fake.rulesetReply(x, "\"v1\"", Fake.body(Fake.flag("on", 1, BOOLS, 1)));
    fake.stream =
        x -> {
          if (attempt.incrementAndGet() % 2 == 1) {
            Fake.reply(x, 503, "{}");
            return;
          }
          OutputStream out = open(x);
          send(out, "event: connected\ndata: \"v1\"\n\n");
          Thread.sleep(15);
          x.close();
        };
    java.util.List<String> torn = new java.util.concurrent.CopyOnWriteArrayList<>();
    java.util.Set<StreamState> states = java.util.concurrent.ConcurrentHashMap.newKeySet();
    try (fake;
        Client client = open(fake, new Tuning(), 20)) {
      assertTrue(client.ready());
      long end = System.nanoTime() + 3_000_000_000L;
      Thread[] readers = new Thread[3];
      for (int i = 0; i < readers.length; i++) {
        readers[i] =
            new Thread(() -> {
              while (System.nanoTime() < end) {
                Status status = client.status();
                states.add(status.stream());
                if (status.stream() == StreamState.connected && status.lastError() != null) {
                  torn.add(status.toString());
                }
              }
            });
        readers[i].start();
      }
      for (Thread t : readers) {
        t.join();
      }
      assertEquals(java.util.List.of(), torn);
      assertTrue(states.contains(StreamState.connected), "" + states);
      assertTrue(states.contains(StreamState.disconnected), "" + states);
      assertTrue(attempt.get() > 10, "attempts " + attempt.get());
    }
  }

  @Test
  void usageSplitsAtUtcMidnight() throws Exception {
    AtomicLong now =
        new AtomicLong(java.time.Instant.parse("2026-01-01T23:59:59.900Z").toEpochMilli());
    Tuning tuning = new Tuning();
    tuning.clock =
        new Clock() {
          @Override
          public long nowMillis() {
            return now.get();
          }

          @Override
          public long monoNanos() {
            return System.nanoTime();
          }
        };
    Fake fake = new Fake();
    fake.ruleset = x -> Fake.rulesetReply(x, "\"v1\"", Fake.body(Fake.flag("on", 1, BOOLS, 1)));
    try (fake) {
      Client client =
          Client.create(
              Options.builder().url(fake.url()).sdkKey("k").stream(false)
                  .flushInterval(Duration.ofMinutes(5))
                  .build(),
              tuning);
      assertTrue(client.ready());
      client.boolVariation("on", Context.of("u"), false);
      now.addAndGet(200);
      client.boolVariation("on", Context.of("u"), false);
      client.boolVariation("on", Context.of("u"), false);
      client.close();
      String body = fake.on("/sdk/v1/events").get(0).body();
      assertTrue(body.contains("\"day\":\"2026-01-01\""), body);
      assertTrue(body.contains("\"day\":\"2026-01-02\""), body);
      assertTrue(body.contains("\"count\":1"), body);
      assertTrue(body.contains("\"count\":2"), body);
      assertFalse(body.contains("\"count\":3"), body);
    }
  }
}
