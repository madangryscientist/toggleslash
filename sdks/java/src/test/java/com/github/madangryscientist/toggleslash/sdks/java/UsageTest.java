package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Json;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

/** Usage counts: the UTC day, batches of 500 and the keep or drop rule. */
class UsageTest {
  @SuppressWarnings("unchecked")
  private static List<Map<String, Object>> events(FakeServer.Request request) {
    Map<String, Object> body = (Map<String, Object>) Json.parse(request.text());
    return (List<Map<String, Object>>) body.get("events");
  }

  @Test
  void countsEitherSideOfAUtcMidnightGoOutAsTwoDays() throws Exception {
    Support.MutableClock clock = new Support.MutableClock(Instant.parse("2026-01-01T23:59:59Z"));
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> {
        if (request.path().equals("/sdk/v1/events")) {
          conn.respond(204, "");
        } else {
          conn.respond(200, Support.boolFlags(), "ETag", "\"v1\"");
        }
      });
      try (Client client =
          Client.create(Support.options(server).stream(false).clock(clock).build())) {
        assertTrue(client.ready());
        client.boolVariation("f", Map.of("kind", "user", "key", "a"), false);
        client.boolVariation("f", Map.of("kind", "user", "key", "b"), false);
        clock.set(Instant.parse("2026-01-02T00:00:01Z"));
        client.boolVariation("f", Map.of("kind", "user", "key", "a"), false);
        client.boolVariation("f", Map.of("kind", "user", "key", "a"), false);
        client.boolVariation("f", Map.of("kind", "user", "key", "a"), false);
        client.flush();
        List<FakeServer.Request> sent = server.requests("/sdk/v1/events");
        assertEquals(1, sent.size());
        List<Map<String, Object>> entries = events(sent.get(0));
        assertEquals(2, entries.size());
        for (Map<String, Object> entry : entries) {
          assertEquals("f", entry.get("flag"));
          assertEquals(1.0, entry.get("variationIndex"));
          if ("2026-01-01".equals(entry.get("day"))) {
            assertEquals(2.0, entry.get("count"));
          } else {
            assertEquals("2026-01-02", entry.get("day"));
            assertEquals(3.0, entry.get("count"));
          }
        }
        // The body names no context: only the flag, the index, the count and the day.
        assertTrue(!sent.get(0).text().contains("user"));
      }
    }
  }

  @Test
  void aFlushSendsAtMost500EntriesAtATime() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> conn.respond(204, ""));
      Options options = Support.options(server).build();
      Core core = new Core(options);
      Usage usage = new Usage(core, new Http(Http.newClient(options), options));
      for (int i = 0; i < 1201; i++) {
        usage.record("flag" + i, 0);
      }
      usage.flush(false);
      List<Integer> sizes = new ArrayList<>();
      for (FakeServer.Request request : server.requests("/sdk/v1/events")) {
        sizes.add(events(request).size());
      }
      assertEquals(List.of(500, 500, 201), sizes);
    }
  }

  @Test
  void anAnswerOf5xxKeepsTheCountsAndStopsAnd4xxDropsThemAndGoesOn() throws Exception {
    AtomicInteger status = new AtomicInteger(500);
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> conn.respond(status.get(), ""));
      Options options = Support.options(server).build();
      Core core = new Core(options);
      Usage usage = new Usage(core, new Http(Http.newClient(options), options));
      for (int i = 0; i < 1001; i++) {
        usage.record("flag" + i, 0);
      }
      // 5xx: the first batch fails and the flush stops, with every count kept.
      usage.flush(false);
      assertEquals(1, server.requests("/sdk/v1/events").size());
      // 4xx: every batch goes, each dropped, and the flush goes on.
      status.set(400);
      usage.flush(false);
      assertEquals(1 + 3, server.requests("/sdk/v1/events").size());
      // Nothing is left to send.
      status.set(204);
      usage.flush(false);
      assertEquals(1 + 3, server.requests("/sdk/v1/events").size());
    }
  }

  @Test
  void countsKeptAfterAFailureAreSentWithTheNextFlush() throws Exception {
    AtomicInteger status = new AtomicInteger(503);
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> conn.respond(status.get(), ""));
      Options options = Support.options(server).build();
      Core core = new Core(options);
      Usage usage = new Usage(core, new Http(Http.newClient(options), options));
      usage.record("f", 1);
      usage.record("f", 1);
      usage.flush(false);
      usage.record("f", 1);
      status.set(204);
      usage.flush(false);
      List<FakeServer.Request> sent = server.requests("/sdk/v1/events");
      assertEquals(2, sent.size());
      assertEquals(3.0, events(sent.get(1)).get(0).get("count"));
    }
  }
}
