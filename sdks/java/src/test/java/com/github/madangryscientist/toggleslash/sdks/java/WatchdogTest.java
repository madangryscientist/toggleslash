package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/**
 * The quiet-stream watchdog, with its length set through a seam, over a real loopback socket that
 * goes quiet: a seam-only test can pass while the timeout never reaches the socket.
 */
@Timeout(60)
final class WatchdogTest {
  private static final long WATCHDOG_MS = 600;

  private FakeServer server;
  private Client client;

  @BeforeEach
  void start() {
    server = new FakeServer();
    server.ruleset =
        FakeServer.serving("\"v1\"", Support.ruleset(Support.boolFlag("checkout", true, 1)));
  }

  @AfterEach
  void stop() {
    if (client != null) {
      client.close();
    }
    server.close();
  }

  private Options.Builder options() {
    return Support.options(server).stream(true)
        .pollInterval(Duration.ofSeconds(30))
        .requestTimeout(Duration.ofSeconds(5))
        .watchdog(Duration.ofMillis(WATCHDOG_MS));
  }

  @Test
  void aStreamThatGetsNoBytesForTheWatchdogIsDroppedAsATimeout() {
    AtomicInteger opened = new AtomicInteger();
    FakeServer.Handler quiet = server.sse("event: connected\ndata: \"v1\"\n\n");
    server.stream =
        (exchange, request) -> {
          if (opened.incrementAndGet() == 1) {
            quiet.handle(exchange, request);
          } else {
            server.hang().handle(exchange, request);
          }
        };
    client = Client.create(options().build());
    Support.until("connected", 5000, () -> client.status().stream() == StreamState.CONNECTED);
    long connectedAt = System.nanoTime();
    Support.until("timeout", 10_000, () -> "timeout".equals(client.status().lastError()));
    long waited = (System.nanoTime() - connectedAt) / 1_000_000L;
    assertTrue(waited >= WATCHDOG_MS - 150, "dropped after " + waited + " ms");
    assertTrue(waited < 5000, "dropped after " + waited + " ms");
    assertNotEquals(StreamState.CONNECTED, client.status().stream());
  }

  @Test
  void aKeepAliveRestartsTheWatchdog() throws Exception {
    AtomicInteger opened = new AtomicInteger();
    server.stream =
        (exchange, request) -> {
          if (opened.incrementAndGet() > 1) {
            server.hang().handle(exchange, request);
            return;
          }
          exchange.getResponseHeaders().add("Content-Type", "text/event-stream");
          exchange.sendResponseHeaders(200, 0);
          java.io.OutputStream out = exchange.getResponseBody();
          out.write("event: connected\ndata: \"v1\"\n\n".getBytes(StandardCharsets.UTF_8));
          out.flush();
          // Four times the watchdog in keep-alives, each well inside it, then silence.
          for (int i = 0; i < 16; i++) {
            Thread.sleep(WATCHDOG_MS / 4);
            out.write(": keep-alive\n\n".getBytes(StandardCharsets.UTF_8));
            out.flush();
          }
          Thread.sleep(60_000);
        };
    client = Client.create(options().build());
    Support.until("connected", 5000, () -> client.status().stream() == StreamState.CONNECTED);
    long end = System.nanoTime() + (long) (WATCHDOG_MS * 2.5 * 1_000_000L);
    while (System.nanoTime() < end) {
      Status status = client.status();
      assertEquals(StreamState.CONNECTED, status.stream());
      assertNull(status.lastError());
      Thread.sleep(30);
    }
    Support.until(
        "timeout after the keep-alives stop",
        10_000,
        () -> "timeout".equals(client.status().lastError()));
  }
}
