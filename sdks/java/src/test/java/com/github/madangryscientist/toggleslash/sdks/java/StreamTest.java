package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Duration;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicLong;
import org.junit.jupiter.api.Test;

/** The stream's quiet-socket watchdog, through the seam that sets its length, on a real socket. */
class StreamTest {
  private static final long WATCHDOG_MILLIS = 400;

  private static boolean streamFailed(List<Throwable> errors) {
    for (Throwable e : errors) {
      if ("stream failed: timeout".equals(e.getMessage())) {
        return true;
      }
    }
    return false;
  }

  @Test
  void aStreamThatGetsNoBytesIsDroppedAsTimeout() throws Exception {
    List<Throwable> errors = new CopyOnWriteArrayList<>();
    AtomicLong connectedAt = new AtomicLong();
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> {
        if (request.path().equals("/sdk/v1/stream")) {
          conn.begin(200, "Content-Type", "text/event-stream");
          conn.write("event: connected\ndata: \"v1\"\n\n");
          connectedAt.compareAndSet(0, System.nanoTime());
          conn.awaitClientGone();
        } else {
          conn.respond(200, Support.boolFlags(), "ETag", "\"v1\"");
        }
      });
      try (Client client =
          Client.create(
              Support.options(server)
                  .watchdog(Duration.ofMillis(WATCHDOG_MILLIS))
                  .onError(errors::add)
                  .build())) {
        assertTrue(client.ready());
        Support.await(
            "the connected state",
            5000,
            () -> "connected".equals(client.status().stream().value()));
        Support.await("the watchdog", 5000, () -> streamFailed(errors));
        long quietMillis = Duration.ofNanos(System.nanoTime() - connectedAt.get()).toMillis();
        assertTrue(
            quietMillis >= WATCHDOG_MILLIS - 50, "dropped after only " + quietMillis + " ms");
        assertTrue(quietMillis < 4000, "dropped after " + quietMillis + " ms");
      }
    }
  }

  @Test
  void aKeepAliveRestartsTheWatchdog() throws Exception {
    List<Throwable> errors = new CopyOnWriteArrayList<>();
    AtomicLong lastWrite = new AtomicLong();
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> {
        if (request.path().equals("/sdk/v1/stream")) {
          conn.begin(200, "Content-Type", "text/event-stream");
          conn.write("event: connected\ndata: \"v1\"\n\n");
          // Three times the watchdog's length in all, in pieces a third of it apart.
          for (int i = 0; i < 9; i++) {
            Thread.sleep(WATCHDOG_MILLIS / 3);
            conn.write(": keep-alive\n\n");
            lastWrite.set(System.nanoTime());
          }
          conn.awaitClientGone();
        } else {
          conn.respond(200, Support.boolFlags(), "ETag", "\"v1\"");
        }
      });
      try (Client client =
          Client.create(
              Support.options(server)
                  .watchdog(Duration.ofMillis(WATCHDOG_MILLIS))
                  .onError(errors::add)
                  .build())) {
        assertTrue(client.ready());
        Support.await("the last keep-alive", 10_000, () -> lastWrite.get() != 0);
        assertFalse(streamFailed(errors), "the watchdog fired during keep-alives: " + errors);
        assertEquals(1, server.requests("/sdk/v1/stream").size());
        assertEquals("connected", client.status().stream().value());
        // The keep-alives stop, and the quiet that follows is dropped.
        Support.await("the watchdog after the keep-alives", 5000, () -> streamFailed(errors));
      }
    }
  }

  @Test
  void aStreamChangedEventFetchesTheNewRulesetAndAFailedFetchEndsTheStream() throws Exception {
    List<Throwable> errors = new CopyOnWriteArrayList<>();
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> {
        if (request.path().equals("/sdk/v1/stream")) {
          conn.begin(200, "Content-Type", "text/event-stream");
          conn.write("event: connected\ndata: \"v1\"\n\n");
          Thread.sleep(200);
          conn.write("event: changed\ndata: \"v2\"\n\n");
          conn.awaitClientGone();
        } else if (server.requests("/sdk/v1/ruleset").size() == 1) {
          conn.respond(200, Support.ruleset(Support.flag("f", "true", 1)), "ETag", "\"v1\"");
        } else {
          conn.respond(503, "down");
        }
      });
      try (Client client = Client.create(Support.options(server).onError(errors::add).build())) {
        assertTrue(client.ready());
        Support.await(
            "the failed fetch", 5000, () -> "http_503".equals(client.status().lastError()));
        // The ruleset held is untouched, and the stream ended (a second one opens after the
        // backoff).
        assertEquals("\"v1\"", client.status().etag());
        Support.await("a second stream", 8000, () -> server.requests("/sdk/v1/stream").size() >= 2);
      }
    }
  }
}
