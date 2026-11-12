package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ReasonKind;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

/**
 * The client against a loopback server: the ordinary paths, and the corners the harness cannot
 * reach.
 */
class ClientTest {
  private static final Map<String, Object> CONTEXT = Map.of("kind", "user", "key", "u1");

  /** Serves the ruleset with an ETag and accepts events. */
  private static FakeServer ordinary(String etag) throws Exception {
    FakeServer server = new FakeServer();
    server.handler((request, conn) -> {
      if (request.path().equals("/sdk/v1/ruleset")) {
        if (etag.equals(request.header("If-None-Match"))) {
          conn.respond(304, "", "ETag", etag);
        } else {
          conn.respond(200, Support.boolFlags(), "ETag", etag, "Content-Type", "application/json");
        }
      } else if (request.path().equals("/sdk/v1/events")) {
        conn.respond(204, "");
      } else {
        conn.respond(404, "");
      }
    });
    return server;
  }

  @Test
  void servesAFlagAfterReady() throws Exception {
    try (FakeServer server = ordinary("\"v1\"");
        Client client = Client.create(Support.options(server).stream(false).build())) {
      assertTrue(client.ready());
      assertTrue(client.boolVariation("f", CONTEXT, false));
      Detail<Boolean> detail = client.boolVariationDetail("f", CONTEXT, false);
      assertEquals(1, detail.variationIndex());
      assertEquals(ReasonKind.FALLTHROUGH, detail.reason().kind());
      assertEquals("\"v1\"", client.status().etag());
      assertEquals("off", client.status().stream().value());
      assertNull(client.status().lastError());
      Detail<Boolean> missing = client.boolVariationDetail("nope", CONTEXT, false);
      assertFalse(missing.value());
      assertNull(missing.variationIndex());
      assertEquals(ErrorCode.FLAG_NOT_FOUND, missing.reason().errorCode());
    }
  }

  @Test
  void answersNotReadyBeforeTheFirstLoad() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> conn.awaitClientGone());
      Client client = Client.create(Support.options(server).stream(false).build());
      long start = System.nanoTime();
      Detail<Boolean> detail = client.boolVariationDetail("f", CONTEXT, true);
      assertTrue(Duration.ofNanos(System.nanoTime() - start).toMillis() < 500);
      assertTrue(detail.value());
      assertEquals(ErrorCode.NOT_READY, detail.reason().errorCode());
      client.close();
      assertFalse(client.ready());
    }
  }

  @Test
  void aConditionalRequestFollowsAnEtagAnd304KeepsTheRuleset() throws Exception {
    try (FakeServer server = ordinary("\"v1\"");
        Client client =
            Client.create(
                Support.options(server).stream(false)
                    .pollInterval(Duration.ofMillis(100))
                    .build())) {
      assertTrue(client.ready());
      Support.await(
          "a conditional request", 5000, () -> server.requests("/sdk/v1/ruleset").size() >= 2);
      List<FakeServer.Request> sent = server.requests("/sdk/v1/ruleset");
      assertNull(sent.get(0).header("If-None-Match"));
      assertEquals("\"v1\"", sent.get(1).header("If-None-Match"));
      assertTrue(client.boolVariation("f", CONTEXT, false));
      assertNull(client.status().lastError());
    }
  }

  @Test
  void aBadRulesetNeverReplacesTheOneHeld() throws Exception {
    AtomicInteger calls = new AtomicInteger();
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> {
        if (calls.incrementAndGet() == 1) {
          conn.respond(200, Support.boolFlags(), "ETag", "\"v1\"");
        } else {
          conn.respond(200, "{\"flags\":{},\"segments\":[]}", "ETag", "\"v2\"");
        }
      });
      try (Client client =
          Client.create(
              Support.options(server).stream(false).pollInterval(Duration.ofMillis(50)).build())) {
        assertTrue(client.ready());
        Support.await(
            "a bad ruleset", 5000, () -> "bad_ruleset".equals(client.status().lastError()));
        assertEquals("\"v1\"", client.status().etag());
        assertTrue(client.boolVariation("f", CONTEXT, false));
      }
    }
  }

  @Test
  void anIntegerAccessorRefusesAFractionAndAnOutOfRangeValue() throws Exception {
    String rules =
        Support.ruleset(
            Support.flag("whole", "2.0", 1),
            Support.flag("fraction", "2.5", 1),
            Support.flag("big", "9223372036854775807", 1),
            Support.flag("edge", "9223372036854774784", 1),
            Support.flag("low", "-9223372036854775808", 1),
            Support.flag("text", "\"x\"", 1));
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> conn.respond(200, rules, "ETag", "\"v1\""));
      try (Client client = Client.create(Support.options(server).stream(false).build())) {
        assertTrue(client.ready());
        assertEquals(2, client.intVariation("whole", CONTEXT, -1));
        assertEquals(-1, client.intVariation("fraction", CONTEXT, -1));
        assertEquals(
            ErrorCode.TYPE_MISMATCH,
            client.intVariationDetail("fraction", CONTEXT, -1).reason().errorCode());
        assertEquals(-1, client.intVariation("big", CONTEXT, -1));
        assertEquals(
            ErrorCode.TYPE_MISMATCH,
            client.intVariationDetail("big", CONTEXT, -1).reason().errorCode());
        // 2^63 - 1024 is the largest integer a double can name below 2^63.
        assertEquals(9223372036854774784L, client.intVariation("edge", CONTEXT, -1));
        assertEquals(Long.MIN_VALUE, client.intVariation("low", CONTEXT, -1));
        assertEquals(-1, client.intVariation("text", CONTEXT, -1));
        assertEquals(2.5, client.floatVariation("fraction", CONTEXT, 0));
        assertEquals("x", client.stringVariation("text", CONTEXT, "d"));
      }
    }
  }

  @Test
  void anEvaluationThatRaisesGivesTheFallbackAndOneOnErrorCall() throws Exception {
    List<Throwable> errors = new CopyOnWriteArrayList<>();
    try (FakeServer server = ordinary("\"v1\"");
        Client client =
            Client.create(
                Support.options(server).stream(false)
                    .onError(errors::add)
                    .evaluate((ruleset, key, context) -> {
                      throw new IllegalStateException("boom");
                    })
                    .build())) {
      assertTrue(client.ready());
      errors.clear();
      Detail<Boolean> detail = client.boolVariationDetail("f", CONTEXT, true);
      assertTrue(detail.value());
      assertNull(detail.variationIndex());
      assertEquals(ReasonKind.ERROR, detail.reason().kind());
      assertEquals(ErrorCode.EVALUATION_FAILED, detail.reason().errorCode());
      assertEquals(1, errors.size());
      client.flush();
      // A failed evaluation is not counted: nothing went out.
      assertTrue(server.requests("/sdk/v1/events").isEmpty());
    }
  }

  @Test
  void aHookAndAnOnErrorThatRaiseChangeNothing() throws Exception {
    AtomicInteger errors = new AtomicInteger();
    try (FakeServer server = ordinary("\"v1\"");
        Client client =
            Client.create(
                Support.options(server).stream(false)
                    .afterEvaluation(event -> {
                      throw new IllegalStateException("hook");
                    })
                    .onError(e -> {
                      errors.incrementAndGet();
                      throw new IllegalStateException("handler");
                    })
                    .build())) {
      assertTrue(client.ready());
      assertTrue(client.boolVariation("f", CONTEXT, false));
      assertTrue(errors.get() >= 1);
    }
  }

  @Test
  void aClientThatIsNeverClosedLeavesOnlyDaemonThreads() throws Exception {
    try (FakeServer server = ordinary("\"v1\"")) {
      Client client = Client.create(Support.options(server).stream(false).build());
      assertTrue(client.ready());
      int sdkThreads = 0;
      for (Thread thread : Thread.getAllStackTraces().keySet()) {
        if (thread.getName().startsWith("toggleslash-")) {
          sdkThreads++;
          assertTrue(thread.isDaemon(), thread.getName() + " must be a daemon thread");
        }
      }
      assertTrue(sdkThreads >= 2);
      client.close();
    }
  }

  @Test
  void anUnusableUrlIsANetworkErrorNotAnException() {
    Client client =
        Client.create(
            Options.builder("not a url", "k").stream(false)
                .requestTimeout(Duration.ofSeconds(2))
                .build());
    assertFalse(client.ready());
    assertEquals("network", client.status().lastError());
    assertNotNull(client.variationDetail("f", CONTEXT, null));
    client.close();
  }

  @Test
  void aTrailingSlashOnTheUrlIsStripped() throws Exception {
    try (FakeServer server = ordinary("\"v1\"");
        Client client =
            Client.create(Options.builder(server.url() + "//", "k").stream(false).build())) {
      assertTrue(client.ready());
      assertEquals("/sdk/v1/ruleset", server.requests().get(0).path());
    }
  }
}
