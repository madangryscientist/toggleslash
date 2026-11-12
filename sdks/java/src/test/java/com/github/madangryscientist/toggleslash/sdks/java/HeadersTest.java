package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** The headers the SDK sends, one request of each route, and what the HTTP client added. */
@Timeout(60)
final class HeadersTest {
  private static final Set<String> SDK =
      Set.of("authorization", "accept", "if-none-match", "content-type");
  private static final Set<String> CLIENT =
      Set.of("host", "user-agent", "content-length", "connection", "accept-encoding");

  @Test
  void everyRouteSendsOnlyTheProtocolsHeadersAndTheHttpClientsOwn() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          FakeServer.serving("\"v1\"", Support.ruleset(Support.boolFlag("checkout", true, 1)));
      server.stream =
          server.sse("event: connected\ndata: \"v1\"\n\n", "event: changed\ndata: \"v2\"\n\n");
      Client client =
          Client.create(
              Support.options(server)
                  .pollInterval(Duration.ofSeconds(30))
                  .flushInterval(Duration.ofHours(1))
                  .requestTimeout(Duration.ofSeconds(5))
                  .build());
      assertTrue(client.ready());
      Support.until(
          "the conditional ruleset request",
          10_000,
          () -> server.to("/sdk/v1/ruleset").size() >= 2);
      client.booleanVariation("checkout", Map.of("kind", "user", "key", "u"), false);
      client.flush();
      client.close();

      Map<String, FakeServer.Req> routes = new TreeMap<>();
      List<FakeServer.Req> rulesets = server.to("/sdk/v1/ruleset");
      routes.put("GET ruleset (first)", rulesets.get(0));
      routes.put("GET ruleset (conditional)", rulesets.get(1));
      routes.put("GET stream", server.to("/sdk/v1/stream").get(0));
      routes.put("POST events", server.to("/sdk/v1/events").get(0));

      assertEquals(null, rulesets.get(0).header("if-none-match"));
      assertEquals("\"v1\"", rulesets.get(1).header("if-none-match"));
      StringBuilder table = new StringBuilder();
      for (Map.Entry<String, FakeServer.Req> route : routes.entrySet()) {
        table.append(route.getKey()).append('\n');
        for (Map.Entry<String, String> header : route.getValue().headers().entrySet()) {
          String name = header.getKey();
          boolean sdk = SDK.contains(name);
          assertTrue(sdk || CLIENT.contains(name), route.getKey() + " sent " + name);
          table
              .append("  ")
              .append(sdk ? "sdk    " : "client ")
              .append(name)
              .append(": ")
              .append(name.equals("authorization") ? "Bearer <key>" : header.getValue())
              .append('\n');
        }
        assertEquals("Bearer " + FakeServer.KEY, route.getValue().header("authorization"));
        assertTrue(route.getValue().header("user-agent").startsWith("Java-http-client/"));
      }
      Path out = Path.of("target", "headers.txt");
      Files.createDirectories(out.getParent());
      Files.writeString(out, table.toString());
    }
  }
}
