package com.github.madangryscientist.toggleslash.sdks.java;

import static com.github.madangryscientist.toggleslash.sdks.java.Support.eventually;
import static com.github.madangryscientist.toggleslash.sdks.java.Support.ms;
import static com.github.madangryscientist.toggleslash.sdks.java.Support.options;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ReasonKind;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.io.OutputStream;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;

class ClientTest {
  private static final Context USER = Context.user("u1");

  @Test
  void loadsAndServes() throws Exception {
    try (FakeServer server = new FakeServer();
        Client client = Client.create(options(server))) {
      assertTrue(client.ready());
      assertTrue(client.readyFuture().get(5, TimeUnit.SECONDS));
      assertTrue(client.boolVariation("f", USER, false));
      Detail<Boolean> detail = client.boolVariationDetail("f", USER, false);
      assertEquals(1, detail.variationIndex());
      assertEquals(ReasonKind.FALLTHROUGH, detail.reason().kind());
      assertEquals("\"v1\"", client.status().etag());
    }
  }

  @Test
  void notReadyGivesTheFallback() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset = server.hang();
      try (Client client = Client.create(options(server).requestTimeout(ms(30_000)))) {
        Detail<Boolean> detail = client.boolVariationDetail("f", USER, true);
        assertTrue(detail.value());
        assertEquals(ErrorCode.NOT_READY, detail.reason().code());
      }
    }
  }

  /** The stress test that stands in for a race detector: Java has none. */
  @Test
  void manyThreadsUseEveryOperationWhileCloseRaces() throws Exception {
    try (FakeServer server = new FakeServer()) {
      Client client = Client.create(options(server).pollInterval(ms(5)).flushInterval(ms(5)));
      assertTrue(client.ready());
      AtomicReference<Throwable> failure = new AtomicReference<>();
      AtomicBoolean stop = new AtomicBoolean();
      List<Thread> threads = new ArrayList<>();
      for (int i = 0; i < 16; i++) {
        Thread t =
            new Thread(() -> {
              try {
                while (!stop.get()) {
                  client.boolVariation("f", USER, false);
                  client.boolVariationDetail("nope", USER, true);
                  client.stringVariation("f", USER, "x");
                  client.doubleVariation("f", USER, 1.5);
                  client.longVariation("f", USER, 3);
                  client.jsonVariation("f", USER, null);
                  client.jsonVariationDetail("f", Context.user("u2"), Json.NULL);
                  client.status();
                  client.ready();
                  client.readyFuture();
                }
              } catch (Throwable e) {
                failure.compareAndSet(null, e);
              }
            });
        threads.add(t);
        t.start();
      }
      Thread.sleep(300);
      CountDownLatch go = new CountDownLatch(1);
      List<Thread> closers = new ArrayList<>();
      for (int i = 0; i < 4; i++) {
        Thread t =
            new Thread(() -> {
              try {
                go.await();
                client.close();
              } catch (Throwable e) {
                failure.compareAndSet(null, e);
              }
            });
        closers.add(t);
        t.start();
      }
      go.countDown();
      for (Thread t : closers) {
        t.join(15_000);
        assertFalse(t.isAlive(), "close returned");
      }
      Thread.sleep(50);
      stop.set(true);
      for (Thread t : threads) {
        t.join(15_000);
        assertFalse(t.isAlive());
      }
      assertNull(failure.get());
      assertEquals(StreamState.CLOSED, client.status().stream());
      assertTrue(client.boolVariation("f", USER, false), "evaluation still answers after close");
    }
  }

  @Test
  void twoClosesRace() throws Exception {
    try (FakeServer server = new FakeServer()) {
      Client client = Client.create(options(server));
      assertTrue(client.ready());
      CountDownLatch go = new CountDownLatch(1);
      Thread a = new Thread(() -> closeAfter(client, go));
      Thread b = new Thread(() -> closeAfter(client, go));
      a.start();
      b.start();
      go.countDown();
      a.join(10_000);
      b.join(10_000);
      assertFalse(a.isAlive());
      assertFalse(b.isAlive());
      client.close();
    }
  }

  private static void closeAfter(Client client, CountDownLatch go) {
    try {
      go.await();
    } catch (InterruptedException e) {
      return;
    }
    client.close();
  }

  @Test
  void closeKeepsToTwiceTheRequestTimeoutWhileRoutesHang() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.events = server.hang();
      Client client = Client.create(options(server).requestTimeout(ms(1000)).flushInterval(ms(20)));
      assertTrue(client.ready());
      client.boolVariation("f", USER, false);
      eventually("an events request", 5000, () -> !server.to("/sdk/v1/events").isEmpty());
      long start = System.nanoTime();
      client.close();
      long took = (System.nanoTime() - start) / 1_000_000;
      assertTrue(took < 3500, "close took " + took + " ms");
    }
  }

  @Test
  void closeAbandonsAHangingRulesetRequest() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset = server.hang();
      Client client = Client.create(options(server).requestTimeout(ms(30_000)));
      eventually("a ruleset request", 5000, () -> !server.to("/sdk/v1/ruleset").isEmpty());
      long start = System.nanoTime();
      client.close();
      long took = (System.nanoTime() - start) / 1_000_000;
      assertTrue(took < 3000, "close took " + took + " ms");
      assertNull(client.status().lastError(), "a failure caused by close is not reported");
    }
  }

  @Test
  void evaluationIsBoundedWhileARulesetRequestHangs() throws Exception {
    try (FakeServer server = new FakeServer()) {
      Client client =
          Client.create(options(server).pollInterval(ms(20)).requestTimeout(ms(30_000)));
      assertTrue(client.ready());
      server.ruleset = server.hang();
      int before = server.to("/sdk/v1/ruleset").size();
      eventually(
          "a hung ruleset request", 5000, () -> server.to("/sdk/v1/ruleset").size() > before);
      long start = System.nanoTime();
      for (int i = 0; i < 2000; i++) {
        assertTrue(client.boolVariation("f", USER, false));
        client.status();
      }
      long took = (System.nanoTime() - start) / 1_000_000;
      assertTrue(took < 1500, "2000 evaluations took " + took + " ms");
      client.close();
    }
  }

  @Test
  void evaluationIsBoundedWhileAUsageFlushHangs() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.events = server.hang();
      Client client = Client.create(options(server).flushInterval(ms(20)).requestTimeout(ms(2000)));
      assertTrue(client.ready());
      client.boolVariation("f", USER, false);
      eventually("a hung events request", 5000, () -> !server.to("/sdk/v1/events").isEmpty());
      long start = System.nanoTime();
      for (int i = 0; i < 2000; i++) {
        assertTrue(client.boolVariation("f", USER, false));
      }
      long took = (System.nanoTime() - start) / 1_000_000;
      assertTrue(took < 1500, "2000 evaluations took " + took + " ms");
      client.close();
    }
  }

  @Test
  void hookEtagAndFlagVersionBelongToTheSameRuleset() throws Exception {
    try (FakeServer server = new FakeServer()) {
      AtomicInteger version = new AtomicInteger(1);
      server.ruleset =
          ex -> {
            int n = version.incrementAndGet();
            ex.getResponseHeaders().set("ETag", "\"v" + n + "\"");
            FakeServer.send(ex, 200, Support.ruleset(Support.flag("f", "true", n)));
          };
      List<String> bad = new CopyOnWriteArrayList<>();
      Set<Long> versions = ConcurrentHashMap.newKeySet();
      Client client =
          Client.create(
              options(server)
                  .pollInterval(ms(2))
                  .afterEvaluation(event -> {
                    if (event.flagVersion() == null) {
                      return;
                    }
                    versions.add(event.flagVersion());
                    if (!event.etag().equals("\"v" + event.flagVersion() + "\"")) {
                      bad.add(event.etag() + " vs " + event.flagVersion());
                    }
                  }));
      assertTrue(client.ready());
      long end = System.nanoTime() + 1_500_000_000L;
      while (System.nanoTime() < end) {
        client.boolVariation("f", USER, false);
      }
      client.close();
      assertTrue(bad.isEmpty(), "unpaired: " + bad);
      assertTrue(versions.size() >= 3, "saw " + versions.size() + " rulesets");
    }
  }

  @Test
  void aHookAndOnErrorMayCallBackIntoTheClient() throws Exception {
    try (FakeServer server = new FakeServer()) {
      ThreadLocal<Boolean> inside = new ThreadLocal<>();
      AtomicReference<Client> ref = new AtomicReference<>();
      AtomicInteger nested = new AtomicInteger();
      Client client =
          Client.create(
              options(server)
                  .afterEvaluation(event -> {
                    if (inside.get() != null) {
                      return;
                    }
                    inside.set(true);
                    try {
                      Client c = ref.get();
                      c.status();
                      c.ready();
                      c.boolVariation("f", USER, false);
                      nested.incrementAndGet();
                    } finally {
                      inside.remove();
                    }
                  }));
      ref.set(client);
      assertTrue(client.ready());
      assertTrue(client.boolVariation("f", USER, false));
      assertEquals(1, nested.get());
      client.close();
    }
  }

  @Test
  void readyOnAnSdkThreadReturnsAtOnceAndCloseFromOnErrorDoesNotDeadlock() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          ex -> {
            Thread.sleep(200);
            FakeServer.send(ex, 500, "");
          };
      AtomicReference<Client> ref = new AtomicReference<>();
      AtomicLong readyNanos = new AtomicLong(-1);
      AtomicBoolean readyAnswer = new AtomicBoolean(true);
      CountDownLatch called = new CountDownLatch(1);
      Client client =
          Client.create(
              options(server)
                  .onError(error -> {
                    if (called.getCount() == 0) {
                      return;
                    }
                    Client c = ref.get();
                    long start = System.nanoTime();
                    readyAnswer.set(c.ready());
                    readyNanos.set(System.nanoTime() - start);
                    c.status();
                    c.close();
                    called.countDown();
                  }));
      ref.set(client);
      assertTrue(called.await(10, TimeUnit.SECONDS), "onError ran");
      assertFalse(readyAnswer.get());
      assertTrue(readyNanos.get() < 100_000_000L, "ready took " + readyNanos.get() + " ns");
      assertTrue(client.awaitClosed(10_000), "the close started from onError finished");
      assertFalse(client.ready());
    }
  }

  private static final class AtomicLong extends java.util.concurrent.atomic.AtomicLong {
    private static final long serialVersionUID = 1L;

    AtomicLong(long value) {
      super(value);
    }
  }

  private static Handler quietStream(FakeServer server, long keepAliveMs, int keepAlives) {
    return ex -> {
      OutputStream out = FakeServer.openStream(ex);
      FakeServer.write(out, "event: connected\ndata: \"v1\"\n\n");
      for (int i = 0; i < keepAlives; i++) {
        Thread.sleep(keepAliveMs);
        FakeServer.write(out, ": keep-alive\n\n");
      }
      server.released.await();
    };
  }

  private interface Handler extends FakeServer.Handler {}

  @Test
  void theWatchdogDropsAStreamThatGoesQuiet() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.stream = quietStream(server, 0, 0)::handle;
      long start = System.nanoTime();
      Client client = Client.create(options(server).stream(true).streamIdle(ms(400)));
      assertTrue(client.ready());
      eventually(
          "a connected stream", 5000, () -> client.status().stream() == StreamState.CONNECTED);
      eventually("the watchdog", 8000, () -> "timeout".equals(client.status().lastError()));
      long took = (System.nanoTime() - start) / 1_000_000;
      assertTrue(took >= 350, "dropped after only " + took + " ms");
      eventually("a reconnect", 8000, () -> server.to("/sdk/v1/stream").size() >= 2);
      client.close();
    }
  }

  @Test
  void aKeepAliveRestartsTheWatchdog() throws Exception {
    try (FakeServer server = new FakeServer()) {
      // 12 comments 150 ms apart keep a 700 ms watchdog quiet for 1.8 s; then the stream goes
      // silent.
      server.stream = quietStream(server, 150, 12)::handle;
      Client client = Client.create(options(server).stream(true).streamIdle(ms(700)));
      assertTrue(client.ready());
      eventually(
          "a connected stream", 5000, () -> client.status().stream() == StreamState.CONNECTED);
      long start = System.nanoTime();
      while ((System.nanoTime() - start) < 1_300_000_000L) {
        assertEquals(StreamState.CONNECTED, client.status().stream());
        assertNull(client.status().lastError());
        Thread.sleep(20);
      }
      eventually(
          "the watchdog after the comments stop",
          8000,
          () -> "timeout".equals(client.status().lastError()));
      client.close();
    }
  }

  @Test
  void retryAfterIsCappedAt300SecondsOnTheRulesetRoute() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset = FakeServer.tooMany("100000");
      Client client = Client.create(options(server));
      assertFalse(client.ready());
      assertEquals("http_429", client.status().lastError());
      long left = client.syncHold().remainingMs();
      assertTrue(left > 295_000 && left <= 300_000, "held " + left);
      client.close();
    }
  }

  @Test
  void retryAfterIsCappedAt300SecondsOnTheStreamRoute() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.stream = FakeServer.tooMany("99999999999999999999999999");
      Client client = Client.create(options(server).stream(true));
      assertTrue(client.ready());
      eventually("the stream answer", 5000, () -> !server.to("/sdk/v1/stream").isEmpty());
      eventually("the hold", 5000, () -> client.syncHold().remainingMs() > 0);
      long left = client.syncHold().remainingMs();
      assertTrue(left > 295_000 && left <= 300_000, "held " + left);
      client.close();
    }
  }

  @Test
  void retryAfterIsCappedAt300SecondsOnTheEventsRoute() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.events = FakeServer.tooMany(" 100000 ");
      Client client = Client.create(options(server).flushInterval(ms(20)));
      assertTrue(client.ready());
      client.boolVariation("f", USER, false);
      eventually("the events hold", 5000, () -> client.usageHold().remainingMs() > 0);
      long left = client.usageHold().remainingMs();
      assertTrue(left > 295_000 && left <= 300_000, "held " + left);
      assertEquals(0, client.syncHold().remainingMs(), "the ruleset hold is separate");
      client.close();
    }
  }

  @Test
  void retryAfterOnAnythingButA429IsIgnored() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          ex -> {
            ex.getResponseHeaders().set("Retry-After", "120");
            FakeServer.send(ex, 503, "");
          };
      Client client = Client.create(options(server));
      assertFalse(client.ready());
      assertEquals("http_503", client.status().lastError());
      assertEquals(0, client.syncHold().remainingMs());
      client.close();
    }
  }

  @Test
  void usageSplitsAtUtcMidnight() throws Exception {
    try (FakeServer server = new FakeServer()) {
      Support.MutableClock clock =
          new Support.MutableClock(Instant.parse("2026-03-01T23:59:59.900Z"));
      Client client = Client.create(options(server).clock(clock));
      assertTrue(client.ready());
      client.boolVariation("f", USER, false);
      client.boolVariation("f", USER, false);
      clock.set(Instant.parse("2026-03-02T00:00:00.100Z"));
      client.boolVariation("f", USER, false);
      client.close();
      List<FakeServer.Seen> posts = server.to("/sdk/v1/events");
      assertEquals(1, posts.size());
      Json.Obj body = (Json.Obj) Json.parse(posts.get(0).body());
      List<String> days = new ArrayList<>();
      for (Json entry : ((Json.Arr) body.get("events")).items()) {
        Json.Obj e = (Json.Obj) entry;
        days.add(
            ((Json.Str) e.get("day")).value() + "=" + (long) ((Json.Num) e.get("count")).value());
      }
      java.util.Collections.sort(days);
      assertEquals(List.of("2026-03-01=2", "2026-03-02=1"), days);
    }
  }

  @Test
  void aRaisedEvaluationGivesTheFallbackOneOnErrorAndNoCount() throws Exception {
    try (FakeServer server = new FakeServer()) {
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      List<EvaluationEvent> events = new CopyOnWriteArrayList<>();
      Client client =
          Client.create(
              options(server)
                  .evaluation((ruleset, key, context) -> {
                    throw new IllegalStateException("boom");
                  })
                  .onError(errors::add)
                  .afterEvaluation(events::add));
      assertTrue(client.ready());
      Detail<Boolean> detail = client.boolVariationDetail("f", USER, true);
      assertTrue(detail.value());
      assertNull(detail.variationIndex());
      assertEquals(ReasonKind.ERROR, detail.reason().kind());
      assertEquals(ErrorCode.EVALUATION_FAILED, detail.reason().code());
      assertEquals(1, errors.size());
      assertEquals(1, events.size());
      assertEquals(ErrorCode.EVALUATION_FAILED, events.get(0).reason().code());
      client.close();
      assertTrue(server.to("/sdk/v1/events").isEmpty(), "nothing was counted");
    }
  }

  @Test
  void theIntegerAccessorKeepsTheInt64Range() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          FakeServer.ok(
              () ->
                  Support.ruleset(
                      Support.flag("big", "4611686018427387904", 1),
                      Support.flag("min", "-9223372036854775808", 1),
                      Support.flag("max", "9223372036854775807", 1),
                      Support.flag("frac", "1.5", 1),
                      Support.flag("two", "2.0", 1),
                      Support.flag("huge", "1e19", 1),
                      Support.flag("hugeNeg", "-1e19", 1),
                      Support.flag("text", "\"x\"", 1),
                      Support.flag("inf", "1e999", 1)),
              "\"v1\"");
      Client client = Client.create(options(server));
      assertTrue(client.ready());
      assertEquals(4611686018427387904L, client.longVariation("big", USER, 0));
      assertEquals(Long.MIN_VALUE, client.longVariation("min", USER, 0));
      assertEquals(2L, client.longVariation("two", USER, 0));
      for (String key : List.of("max", "frac", "huge", "hugeNeg", "text", "inf")) {
        Detail<Long> d = client.longVariationDetail(key, USER, -5);
        assertEquals(-5L, d.value(), key);
        assertEquals(ErrorCode.TYPE_MISMATCH, d.reason().code(), key);
      }
      assertEquals(Double.POSITIVE_INFINITY, client.doubleVariation("inf", USER, 0));
      assertEquals(1.5, client.doubleVariation("frac", USER, 0));
      client.close();
    }
  }

  @Test
  void aNullFallbackAcceptsAnyKindOnTheJsonAccessor() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          FakeServer.ok(
              () ->
                  Support.ruleset(
                      Support.flag("arr", "[1]", 1),
                      Support.flag("obj", "{\"a\":1}", 1),
                      Support.flag("nul", "null", 1)),
              "\"v1\"");
      Client client = Client.create(options(server));
      assertTrue(client.ready());
      assertEquals(Json.parse("[1]"), client.jsonVariation("arr", USER, null));
      assertEquals(Json.parse("[1]"), client.jsonVariation("arr", USER, Json.NULL));
      assertEquals(Json.parse("[1]"), client.jsonVariation("arr", USER, Json.parse("[]")));
      assertEquals(
          ErrorCode.TYPE_MISMATCH,
          client.jsonVariationDetail("arr", USER, Json.parse("{}")).reason().code());
      assertEquals(
          ErrorCode.TYPE_MISMATCH,
          client.jsonVariationDetail("obj", USER, Json.parse("[]")).reason().code());
      assertEquals(
          ErrorCode.TYPE_MISMATCH,
          client.jsonVariationDetail("nul", USER, Json.parse("[]")).reason().code());
      assertEquals(Json.NULL, client.jsonVariation("nul", USER, null));
      client.close();
    }
  }
}
