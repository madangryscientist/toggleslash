package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import org.junit.jupiter.api.Test;

/**
 * One request of each route, as the client sends it: the headers must be the protocol's and no
 * more.
 */
class HeadersTest {
  /** The headers the SDK sets itself. */
  private static final Set<String> OURS =
      Set.of("authorization", "accept", "if-none-match", "content-type");

  /** What the JDK's HTTP client adds on its own; RUN.md lists why each one is safe. */
  private static final Set<String> CLIENT_ADDED =
      Set.of("host", "user-agent", "content-length", "connection", "accept-encoding");

  private static String row(String route, FakeServer.Request request) {
    List<String> ours = new ArrayList<>();
    List<String> added = new ArrayList<>();
    for (FakeServer.Header header : request.headers()) {
      String name = header.name().toLowerCase(Locale.ROOT);
      boolean mine = OURS.contains(name);
      assertTrue(
          mine || CLIENT_ADDED.contains(name), route + " carries an unexpected header " + name);
      String shown =
          header.name() + (name.equals("authorization") ? ": Bearer <key>" : ": " + header.value());
      (mine ? ours : added).add(shown);
    }
    return "| " + route + " | " + String.join("; ", ours) + " | " + String.join("; ", added) + " |";
  }

  @Test
  void everyRouteSendsOnlyTheProtocolsHeaders() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> {
        switch (request.path()) {
          case "/sdk/v1/ruleset" -> {
            if ("\"v1\"".equals(request.header("If-None-Match"))) {
              conn.respond(304, "", "ETag", "\"v1\"");
            } else {
              conn.respond(200, Support.boolFlags(), "ETag", "\"v1\"");
            }
          }
          case "/sdk/v1/events" -> conn.respond(204, "");
          default -> {
            conn.begin(200, "Content-Type", "text/event-stream");
            conn.write("event: connected\ndata: \"v1\"\n\n");
            conn.awaitClientGone();
          }
        }
      });
      try (Client client =
          Client.create(Support.options(server).pollInterval(Duration.ofMillis(100)).build())) {
        assertTrue(client.ready());
        client.boolVariation("f", Map.of("kind", "user", "key", "who-am-i"), false);
        client.flush();
        Support.await("a stream request", 5000, () -> !server.requests("/sdk/v1/stream").isEmpty());
      }
      try (Client client =
          Client.create(
              Support.options(server).stream(false).pollInterval(Duration.ofMillis(100)).build())) {
        assertTrue(client.ready());
        Support.await(
            "a conditional request",
            5000,
            () ->
                server.requests("/sdk/v1/ruleset").stream()
                    .anyMatch(r -> r.header("If-None-Match") != null));
      }
      List<String> rows = new ArrayList<>();
      rows.add(row("GET /sdk/v1/ruleset", server.requests("/sdk/v1/ruleset").get(0)));
      for (FakeServer.Request r : server.requests("/sdk/v1/ruleset")) {
        if (r.header("If-None-Match") != null) {
          rows.add(row("GET /sdk/v1/ruleset (conditional)", r));
          break;
        }
      }
      rows.add(row("GET /sdk/v1/stream", server.requests("/sdk/v1/stream").get(0)));
      FakeServer.Request events = server.requests("/sdk/v1/events").get(0);
      rows.add(row("POST /sdk/v1/events", events));
      assertEquals("application/json", events.header("Content-Type"));
      assertEquals(
          "Bearer test-key", server.requests("/sdk/v1/ruleset").get(0).header("Authorization"));
      assertEquals("text/event-stream", server.requests("/sdk/v1/stream").get(0).header("Accept"));
      for (FakeServer.Request r : server.requests()) {
        String agent = r.header("User-Agent");
        assertTrue(
            agent == null || agent.startsWith("Java-http-client/"),
            "the User-Agent names more than the library: " + agent);
        assertTrue(!r.text().contains("who-am-i"));
      }
      Path out = Path.of("target", "headers-table.md");
      Files.createDirectories(out.getParent());
      Files.write(out, rows, StandardCharsets.UTF_8);
      rows.forEach(System.out::println);
    }
  }
}
