package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.testkit.Fake;
import java.math.BigDecimal;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.function.Consumer;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** The client against a scripted loopback server. */
@Timeout(60)
class ClientTest {
  private static final String BOOLS = "[false,true]";

  private static Client open(Fake fake, Consumer<Options.Builder> tweak) {
    Options.Builder b =
        Options.builder().url(fake.url()).sdkKey("sdk-key-1").stream(false)
            .requestTimeout(Duration.ofSeconds(3));
    tweak.accept(b);
    return Client.create(b.build());
  }

  private static Fake serving(String body) throws Exception {
    Fake fake = new Fake();
    fake.ruleset = x -> Fake.rulesetReply(x, "\"v1\"", body);
    return fake;
  }

  @Test
  void servesTheHeldRulesetAndReportsStatus() throws Exception {
    String body =
        Fake.body(
            Fake.flag("on", 4, BOOLS, 1),
            Fake.flag("name", 1, "[\"a\",\"b\"]", 1),
            Fake.flag("count", 1, "[1,7]", 1),
            Fake.flag("ratio", 1, "[0.5,2.5]", 1),
            Fake.flag("blob", 1, "[{},{\"k\":[1,2]}]", 1));
    try (Fake fake = serving(body);
        Client client = open(fake, b -> {})) {
      assertTrue(client.ready());
      Object ctx = Context.of("u1");
      assertTrue(client.boolVariation("on", ctx, false));
      assertEquals("b", client.stringVariation("name", ctx, "x"));
      assertEquals(7L, client.intVariation("count", ctx, 0));
      assertEquals(2.5, client.floatVariation("ratio", ctx, 0));
      assertEquals(Map.of("k", List.of(1.0, 2.0)), client.jsonVariation("blob", ctx, null));
      Detail<Boolean> detail = client.boolVariationDetail("on", ctx, false);
      assertEquals(1, detail.variationIndex());
      assertEquals(ReasonKind.FALLTHROUGH, detail.reason().kind());
      Status status = client.status();
      assertEquals("\"v1\"", status.etag());
      assertEquals(StreamState.off, status.stream());
      assertNull(status.lastError());
      assertNotNull(status.lastSyncAt());
    }
  }

  @Test
  void aMissingFlagAndAKindMismatchAnswerTheFallback() throws Exception {
    try (Fake fake = serving(Fake.body(Fake.flag("on", 1, BOOLS, 1)));
        Client client = open(fake, b -> {})) {
      assertTrue(client.ready());
      Detail<String> missing = client.stringVariationDetail("nope", Context.of("u"), "fb");
      assertEquals("fb", missing.value());
      assertNull(missing.variationIndex());
      assertEquals(ErrorCode.FLAG_NOT_FOUND, missing.reason().errorCode());
      Detail<String> wrong = client.stringVariationDetail("on", Context.of("u"), "fb");
      assertEquals("fb", wrong.value());
      assertEquals(ErrorCode.TYPE_MISMATCH, wrong.reason().errorCode());
    }
  }

  @Test
  void beforeTheFirstLoadTheAnswerIsNotReadyAndNothingBlocks() throws Exception {
    Fake fake = new Fake();
    java.util.concurrent.CountDownLatch release = new java.util.concurrent.CountDownLatch(1);
    fake.ruleset =
        x -> {
          release.await();
          Fake.rulesetReply(x, "\"v1\"", Fake.body(Fake.flag("on", 1, BOOLS, 1)));
        };
    try (fake;
        Client client = open(fake, b -> b.requestTimeout(Duration.ofSeconds(30)))) {
      long start = System.nanoTime();
      for (int i = 0; i < 100; i++) {
        Detail<Boolean> d = client.boolVariationDetail("on", Context.of("u"), false);
        assertEquals(ErrorCode.NOT_READY, d.reason().errorCode());
        assertFalse(d.value());
      }
      client.status();
      assertTrue(System.nanoTime() - start < 500_000_000L, "variation waited on the network");
      release.countDown();
      assertTrue(client.ready());
      assertTrue(client.boolVariation("on", Context.of("u"), false));
    }
  }

  @Test
  void readyAnswersFalseWhenTheRulesetNeverComes() throws Exception {
    Fake fake = new Fake();
    java.util.concurrent.CountDownLatch release = new java.util.concurrent.CountDownLatch(1);
    fake.ruleset =
        x -> {
          release.await();
          Fake.reply(x, 200, "{}");
        };
    try (fake;
        Client client = open(fake, b -> b.requestTimeout(Duration.ofMillis(300)))) {
      long start = System.nanoTime();
      assertFalse(client.ready());
      assertTrue(System.nanoTime() - start < 3_000_000_000L);
      assertEquals("timeout", client.status().lastError());
      assertFalse(client.ready());
      release.countDown();
    }
  }

  @Test
  void aSlowFlushDoesNotBlockVariation() throws Exception {
    Fake fake = serving(Fake.body(Fake.flag("on", 1, BOOLS, 1)));
    java.util.concurrent.CountDownLatch release = new java.util.concurrent.CountDownLatch(1);
    fake.events =
        x -> {
          release.await();
          Fake.reply(x, 200, "{}");
        };
    try (fake;
        Client client =
            open(
                fake,
                b ->
                    b.flushInterval(Duration.ofMillis(50))
                        .requestTimeout(Duration.ofSeconds(30)))) {
      assertTrue(client.ready());
      client.boolVariation("on", Context.of("u"), false);
      assertTrue(
          fake.await(3000, s -> s.stream().anyMatch(r -> r.path().equals("/sdk/v1/events"))));
      long start = System.nanoTime();
      for (int i = 0; i < 1000; i++) {
        client.boolVariation("on", Context.of("u"), false);
      }
      assertTrue(System.nanoTime() - start < 1_000_000_000L);
      release.countDown();
    }
  }

  @Test
  void requestsCarryOnlyTheKeyAndNeverTheContext() throws Exception {
    String sentinel = "sentinel-user-9f3a";
    try (Fake fake = serving(Fake.body(Fake.flag("on", 1, BOOLS, 1)))) {
      Client client =
          open(
              fake,
              b -> b.pollInterval(Duration.ofMillis(60)).flushInterval(Duration.ofMillis(60)));
      assertTrue(client.ready());
      Object ctx = Context.with(Context.of(sentinel), "email", sentinel + "@example.com");
      client.boolVariation("on", ctx, false);
      assertTrue(
          fake.await(3000, s -> s.stream().anyMatch(r -> r.path().equals("/sdk/v1/events"))));
      assertTrue(
          fake.await(
              3000, s -> s.stream().filter(r -> r.path().equals("/sdk/v1/ruleset")).count() >= 2));
      client.close();
      Set<String> allowed =
          Set.of(
              "authorization",
              "if-none-match",
              "content-type",
              "content-length",
              "user-agent",
              "host",
              "accept",
              "connection");
      for (Fake.Seen request : fake.seen) {
        assertEquals("Bearer sdk-key-1", request.header("Authorization"));
        assertFalse(request.path().contains(sentinel));
        assertFalse(request.body().contains(sentinel));
        for (Map.Entry<String, List<String>> h : request.headers().entrySet()) {
          assertTrue(allowed.contains(h.getKey().toLowerCase()), "header " + h.getKey());
          for (String v : h.getValue()) {
            assertFalse(v.contains(sentinel));
          }
        }
      }
      assertNull(fake.on("/sdk/v1/ruleset").get(0).header("If-None-Match"));
      assertEquals("\"v1\"", fake.on("/sdk/v1/ruleset").get(1).header("If-None-Match"));
      assertEquals("application/json", fake.on("/sdk/v1/events").get(0).header("Content-Type"));
    }
  }

  @Test
  void closeSendsTheFinalFlushAndIsIdempotent() throws Exception {
    try (Fake fake = serving(Fake.body(Fake.flag("on", 1, BOOLS, 1)))) {
      Client client = open(fake, b -> b.flushInterval(Duration.ofSeconds(60)));
      assertTrue(client.ready());
      for (int i = 0; i < 3; i++) {
        client.boolVariation("on", Context.of("u"), false);
      }
      client.close();
      client.close();
      List<Fake.Seen> events = fake.on("/sdk/v1/events");
      assertEquals(1, events.size());
      assertTrue(events.get(0).body().contains("\"count\":3"), events.get(0).body());
      assertEquals(StreamState.closed, client.status().stream());
      Detail<Boolean> after = client.boolVariationDetail("on", Context.of("u"), false);
      assertEquals(ReasonKind.FALLTHROUGH, after.reason().kind(), "a closed client still answers");
    }
  }

  @Test
  void closeAbandonsAHangingRulesetRequest() throws Exception {
    Fake fake = new Fake();
    java.util.concurrent.CountDownLatch release = new java.util.concurrent.CountDownLatch(1);
    fake.ruleset =
        x -> {
          release.await();
          Fake.reply(x, 200, "{}");
        };
    try (fake) {
      Client client = open(fake, b -> b.requestTimeout(Duration.ofSeconds(30)));
      fake.await(3000, s -> !s.isEmpty());
      long start = System.nanoTime();
      client.close();
      assertTrue(System.nanoTime() - start < 2_000_000_000L);
      assertFalse(client.ready());
      release.countDown();
    }
  }

  @Test
  void closeRacingVariationAndRacingClosesEndCleanly() throws Exception {
    for (int round = 0; round < 20; round++) {
      try (Fake fake = serving(Fake.body(Fake.flag("on", 1, BOOLS, 1)))) {
        Client client = open(fake, b -> b.flushInterval(Duration.ofMillis(5)));
        assertTrue(client.ready());
        List<Throwable> failures = new CopyOnWriteArrayList<>();
        java.util.concurrent.CountDownLatch go = new java.util.concurrent.CountDownLatch(1);
        List<Thread> threads = new java.util.ArrayList<>();
        for (int i = 0; i < 6; i++) {
          Thread t =
              new Thread(() -> {
                try {
                  go.await();
                  for (int j = 0; j < 2000; j++) {
                    client.boolVariation("on", Context.of("u" + j), false);
                    client.status();
                  }
                } catch (Throwable e) {
                  failures.add(e);
                }
              });
          threads.add(t);
          t.start();
        }
        for (int i = 0; i < 3; i++) {
          Thread t =
              new Thread(() -> {
                try {
                  go.await();
                  Thread.sleep(2);
                  client.close();
                } catch (Throwable e) {
                  failures.add(e);
                }
              });
          threads.add(t);
          t.start();
        }
        go.countDown();
        for (Thread t : threads) {
          t.join(20_000);
          assertFalse(t.isAlive());
        }
        assertEquals(List.of(), failures);
        assertEquals(StreamState.closed, client.status().stream());
        long after = fake.seen.size();
        Thread.sleep(30);
        assertEquals(after, fake.seen.size(), "a request left after close returned");
      }
    }
  }

  @Test
  void aRulesetSwapNeverShowsATornPair() throws Exception {
    java.util.concurrent.atomic.AtomicInteger n = new java.util.concurrent.atomic.AtomicInteger();
    Fake fake = new Fake();
    fake.ruleset =
        x -> {
          int v = n.incrementAndGet();
          Fake.rulesetReply(x, "\"v" + v + "\"", Fake.body(Fake.flag("on", v, BOOLS, 1)));
        };
    List<String> torn = new CopyOnWriteArrayList<>();
    Hook hook =
        e -> {
          if (e.etag() != null && e.flagVersion() != null) {
            String want = "\"v" + e.flagVersion() + "\"";
            if (!want.equals(e.etag())) {
              torn.add(e.etag() + " / " + e.flagVersion());
            }
          }
          return null;
        };
    try (fake;
        Client client =
            open(fake, b -> b.pollInterval(Duration.ofMillis(5)).afterEvaluation(hook))) {
      assertTrue(client.ready());
      long end = System.nanoTime() + 1_500_000_000L;
      Thread[] readers = new Thread[4];
      for (int i = 0; i < readers.length; i++) {
        readers[i] =
            new Thread(() -> {
              while (System.nanoTime() < end) {
                client.boolVariation("on", Context.of("u"), false);
              }
            });
        readers[i].start();
      }
      for (Thread t : readers) {
        t.join();
      }
      assertTrue(n.get() > 20, "the ruleset swapped " + n.get() + " times");
      assertEquals(List.of(), torn);
    }
  }

  @Test
  void aHookSeesTheEvaluationAndItsFailuresGoToOnError() throws Exception {
    List<EvaluationEvent> events = new CopyOnWriteArrayList<>();
    List<Throwable> errors = new CopyOnWriteArrayList<>();
    Thread caller = Thread.currentThread();
    List<Thread> hookThreads = new CopyOnWriteArrayList<>();
    Hook hook =
        e -> {
          events.add(e);
          hookThreads.add(Thread.currentThread());
          if ("boom".equals(e.flagKey())) {
            throw new IllegalStateException("hook failed");
          }
          if ("late".equals(e.flagKey())) {
            return CompletableFuture.failedFuture(new IllegalArgumentException("late failure"));
          }
          return null;
        };
    try (Fake fake =
            serving(
                Fake.body(
                    Fake.flag("count", 3, "[1,7]", 1),
                    Fake.flag("boom", 1, BOOLS, 1),
                    Fake.flag("late", 1, BOOLS, 1)));
        Client client = open(fake, b -> b.afterEvaluation(hook).onError(errors::add))) {
      assertTrue(client.ready());
      assertEquals(7L, client.intVariation("count", Context.of("u"), 0));
      assertEquals(1, events.size());
      EvaluationEvent e = events.get(0);
      assertEquals("count", e.flagKey());
      assertEquals(7L, e.value());
      assertEquals(1, e.variationIndex());
      assertEquals("\"v1\"", e.etag());
      assertEquals(3L, e.flagVersion());
      assertEquals(caller, hookThreads.get(0));
      assertTrue(client.boolVariation("boom", Context.of("u"), false));
      assertEquals(1, errors.size());
      assertEquals("hook failed", errors.get(0).getMessage());
      assertTrue(client.boolVariation("late", Context.of("u"), false));
      assertEquals(2, errors.size());
      assertInstanceOf(IllegalArgumentException.class, errors.get(1));
    }
  }

  @Test
  void anEvaluationThatRaisesAnswersEvaluationFailedWithOneOnError() throws Exception {
    List<Throwable> errors = new CopyOnWriteArrayList<>();
    Map<String, Object> hostile =
        new java.util.AbstractMap<>() {
          @Override
          public Set<Map.Entry<String, Object>> entrySet() {
            throw new IllegalStateException("hostile map");
          }

          @Override
          public Object get(Object key) {
            throw new IllegalStateException("hostile map");
          }

          @Override
          public boolean containsKey(Object key) {
            throw new IllegalStateException("hostile map");
          }
        };
    try (Fake fake = serving(Fake.body(Fake.flag("on", 1, BOOLS, 1)));
        Client client = open(fake, b -> b.onError(errors::add))) {
      assertTrue(client.ready());
      Detail<Boolean> d = client.boolVariationDetail("on", hostile, false);
      assertFalse(d.value());
      assertNull(d.variationIndex());
      assertEquals(ReasonKind.ERROR, d.reason().kind());
      Set<ErrorCode> allowed = Set.of(ErrorCode.EVALUATION_FAILED, ErrorCode.INVALID_CONTEXT);
      assertTrue(allowed.contains(d.reason().errorCode()), "" + d.reason());
      if (d.reason().errorCode() == ErrorCode.EVALUATION_FAILED) {
        assertEquals(1, errors.size());
      }
    }
  }

  @Test
  void integerAccessorsCheckTheRange() throws Exception {
    try (Fake fake =
            serving(
                Fake.body(
                    Fake.flag("frac", 1, "[0,1.5]", 1),
                    Fake.flag("huge", 1, "[0,9223372036854775808]", 1),
                    Fake.flag("big", 1, "[0,3000000000]", 1),
                    Fake.flag("ok", 1, "[0,12]", 1),
                    Fake.flag("neg", 1, "[0,-9223372036854775808]", 1)));
        Client client = open(fake, b -> {})) {
      assertTrue(client.ready());
      Object ctx = Context.of("u");
      assertEquals(5L, client.intVariation("frac", ctx, 5));
      assertEquals(
          ErrorCode.TYPE_MISMATCH, client.intVariationDetail("frac", ctx, 5).reason().errorCode());
      assertEquals(5L, client.intVariation("huge", ctx, 5));
      assertEquals(12L, client.intVariation("ok", ctx, 5));
      assertEquals(Long.MIN_VALUE, client.intVariation("neg", ctx, 5));
      assertEquals(3_000_000_000L, client.intVariation("big", ctx, 5));
      assertEquals(Integer.valueOf(12), client.variation("ok", ctx, Integer.valueOf(5)));
      assertEquals(Integer.valueOf(5), client.variation("big", ctx, Integer.valueOf(5)));
      assertEquals(Long.valueOf(3_000_000_000L), client.variation("big", ctx, Long.valueOf(5)));
      assertEquals(
          Short.valueOf((short) 12), client.variation("ok", ctx, Short.valueOf((short) 5)));
      assertEquals(Byte.valueOf((byte) 12), client.variation("ok", ctx, Byte.valueOf((byte) 5)));
      assertEquals(
          Short.valueOf((short) 5), client.variation("big", ctx, Short.valueOf((short) 5)));
      assertEquals(12.0, client.variation("ok", ctx, 5.5));
      assertEquals(
          ErrorCode.TYPE_MISMATCH,
          client.variationDetail("frac", ctx, Integer.valueOf(5)).reason().errorCode());
    }
  }

  @Test
  void genericAccessorsHandleNullFallbacksAndClassesWithNoJsonKind() throws Exception {
    try (Fake fake =
            serving(
                Fake.body(
                    Fake.flag("name", 1, "[\"a\",\"b\"]", 1),
                    Fake.flag("count", 1, "[1,7]", 1),
                    Fake.flag("blob", 1, "[{},{\"k\":1}]", 1)));
        Client client = open(fake, b -> {})) {
      assertTrue(client.ready());
      Object ctx = Context.of("u");
      assertEquals("b", client.stringVariation("name", ctx, null));
      Detail<Object> any = client.variationDetail("name", ctx, null);
      assertEquals("b", any.value());
      Detail<BigDecimal> decimal = client.variationDetail("count", ctx, BigDecimal.ONE);
      assertEquals(BigDecimal.ONE, decimal.value());
      assertEquals(ErrorCode.TYPE_MISMATCH, decimal.reason().errorCode());
      Object json = client.variation("blob", ctx, Map.of());
      assertEquals(Map.of("k", 1.0), json);
      assertEquals("fb", client.variation("count", ctx, "fb"));
    }
  }

  @Test
  void aStringAccessorWithANullFallbackAcceptsOnlyAString() throws Exception {
    try (Fake fake =
            serving(
                Fake.body(
                    Fake.flag("name", 1, "[\"a\",\"b\"]", 1), Fake.flag("count", 1, "[1,7]", 1)));
        Client client = open(fake, b -> {})) {
      assertTrue(client.ready());
      Object ctx = Context.of("u");
      Detail<String> served = client.stringVariationDetail("name", ctx, null);
      assertEquals("b", served.value());
      Detail<String> mismatch = client.stringVariationDetail("count", ctx, null);
      assertNull(mismatch.value());
      assertEquals(ErrorCode.TYPE_MISMATCH, mismatch.reason().errorCode());
      assertNull(client.stringVariation("count", ctx, null));
    }
  }

  @Test
  void aServedJsonTreeIsReadOnly() throws Exception {
    try (Fake fake = serving(Fake.body(Fake.flag("blob", 1, "[{},{\"k\":[1]}]", 1)));
        Client client = open(fake, b -> {})) {
      assertTrue(client.ready());
      @SuppressWarnings("unchecked")
      Map<String, Object> json =
          (Map<String, Object>) client.jsonVariation("blob", Context.of("u"), null);
      boolean threw = false;
      try {
        json.put("x", 1);
      } catch (UnsupportedOperationException e) {
        threw = true;
      }
      assertTrue(threw);
      boolean threwList = false;
      try {
        @SuppressWarnings("unchecked")
        List<Object> list = (List<Object>) json.get("k");
        list.add(2);
      } catch (UnsupportedOperationException e) {
        threwList = true;
      }
      assertTrue(threwList);
    }
  }

  @Test
  void contextAttributesOfEveryNumericTypeMatchAWholeNumber() throws Exception {
    String flag =
        "{\"key\":\"n\",\"variations\":[false,true],\"on\":true,\"offVariation\":0,\"targets\":[],"
            + "\"prerequisites\":[],\"rules\":[{\"clauses\":[{\"attribute\":\"n\",\"op\":\"in\","
            + "\"values\":[5]}],\"variation\":1}],\"fallthrough\":{\"variation\":0},\"salt\":\"s\","
            + "\"version\":1}";
    try (Fake fake = serving(Fake.body(flag));
        Client client = open(fake, b -> {})) {
      assertTrue(client.ready());
      Object[] fives = {
        5,
        5L,
        (short) 5,
        (byte) 5,
        5.0,
        5.0f,
        new BigDecimal("5.0"),
        java.math.BigInteger.valueOf(5)
      };
      for (Object five : fives) {
        Object ctx = Context.with(Context.of("u"), "n", five);
        assertTrue(client.boolVariation("n", ctx, false), five.getClass().getName());
      }
      Object other = Context.with(Context.of("u"), "n", 6);
      assertFalse(client.boolVariation("n", other, false));
    }
  }

  @Test
  void callbacksMayCallBackIntoTheClient() throws Exception {
    Fake fake = new Fake();
    fake.ruleset = x -> Fake.reply(x, 500, "{}");
    java.util.concurrent.CountDownLatch seen = new java.util.concurrent.CountDownLatch(1);
    List<Object> inside = new CopyOnWriteArrayList<>();
    Client[] holder = new Client[1];
    Consumer<Throwable> onError =
        t -> {
          Client c = holder[0];
          if (c == null) {
            return;
          }
          inside.add(c.ready());
          inside.add(c.status().lastError());
          inside.add(c.boolVariation("any", Context.of("u"), true));
          c.close();
          seen.countDown();
        };
    try (fake) {
      holder[0] = open(fake, b -> b.onError(onError).requestTimeout(Duration.ofSeconds(30)));
      assertTrue(seen.await(10, java.util.concurrent.TimeUnit.SECONDS));
      assertEquals(Boolean.FALSE, inside.get(0));
      assertEquals(Boolean.TRUE, inside.get(2));
      holder[0].close();
      assertEquals(StreamState.closed, holder[0].status().stream());
    }
  }

  @Test
  void daemonThreadsOnlyAndNoThreadOutlivesClose() throws Exception {
    try (Fake fake = serving(Fake.body(Fake.flag("on", 1, BOOLS, 1)))) {
      Client client = open(fake, b -> {});
      assertTrue(client.ready());
      for (Thread t : Thread.getAllStackTraces().keySet()) {
        if (t.getName().startsWith("toggleslash-")) {
          assertTrue(t.isDaemon(), t.getName());
        }
      }
      client.close();
      long end = System.nanoTime() + 3_000_000_000L;
      long left;
      do {
        left =
            Thread.getAllStackTraces().keySet().stream()
                .filter(t -> t.getName().startsWith("toggleslash-") && t.isAlive())
                .count();
        if (left > 0) {
          Thread.sleep(10);
        }
      } while (left > 0 && System.nanoTime() < end);
      assertEquals(0, left);
    }
  }

  @Test
  void aBadUrlNeverThrows() {
    Client client =
        Client.create(Options.builder().url("not a url").sdkKey("k").stream(false).build());
    assertFalse(client.ready());
    assertFalse(client.boolVariation("x", Context.of("u"), false));
    client.close();
    Client none = Client.create(Options.builder().build());
    assertFalse(none.ready());
    none.close();
  }
}
