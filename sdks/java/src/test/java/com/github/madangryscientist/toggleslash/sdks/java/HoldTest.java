package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;

import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;

/** A {@code Retry-After} above 300 is held for 300 s on the ruleset, stream and events routes. */
class HoldTest {
  private static final long CAP_NANOS = TimeUnit.SECONDS.toNanos(300);

  private static FakeServer tooMany(String retryAfter) throws Exception {
    FakeServer server = new FakeServer();
    server.handler((request, conn) -> conn.respond(429, "slow down", "Retry-After", retryAfter));
    return server;
  }

  @Test
  void theRulesetRouteHoldsForTheCap() throws Exception {
    try (FakeServer server = tooMany("100000")) {
      Support.TestPacer pacer = new Support.TestPacer(false);
      Options options = Support.options(server).pacer(pacer).build();
      Core core = new Core(options);
      Http http = new Http(Http.newClient(options), options);
      SyncLoop sync = new SyncLoop(core, http);
      sync.firstLoad();
      assertEquals(CAP_NANOS, core.rulesetHold.remainingNanos());
      assertEquals("http_429", core.state.get().lastError());
      // The hold runs down with the monotonic clock.
      pacer.advance(java.time.Duration.ofSeconds(299));
      assertEquals(TimeUnit.SECONDS.toNanos(1), core.rulesetHold.remainingNanos());
      pacer.advance(java.time.Duration.ofSeconds(2));
      assertEquals(0, core.rulesetHold.remainingNanos());
    }
  }

  @Test
  void aValueBelowTheCapIsHeldAsGiven() throws Exception {
    try (FakeServer server = tooMany("7")) {
      Options options = Support.options(server).pacer(new Support.TestPacer(false)).build();
      Core core = new Core(options);
      new SyncLoop(core, new Http(Http.newClient(options), options)).firstLoad();
      assertEquals(TimeUnit.SECONDS.toNanos(7), core.rulesetHold.remainingNanos());
    }
  }

  @Test
  void anHttpDateOrAFractionIsNotHeld() throws Exception {
    for (String value : new String[] {"Wed, 21 Oct 2099 07:28:00 GMT", "1.5", "-3", ""}) {
      try (FakeServer server = tooMany(value)) {
        Options options = Support.options(server).pacer(new Support.TestPacer(false)).build();
        Core core = new Core(options);
        new SyncLoop(core, new Http(Http.newClient(options), options)).firstLoad();
        assertEquals(0, core.rulesetHold.remainingNanos(), "Retry-After: " + value);
      }
    }
  }

  @Test
  void theStreamRouteHoldsForTheCapForTheRulesetToo() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.handler((request, conn) -> {
        if (request.path().equals("/sdk/v1/stream")) {
          conn.respond(429, "slow down", "Retry-After", "100000");
        } else {
          conn.respond(200, Support.boolFlags(), "ETag", "\"v1\"");
        }
      });
      Options options = Support.options(server).pacer(new Support.TestPacer(false)).build();
      Core core = new Core(options);
      SyncLoop sync = new SyncLoop(core, new Http(Http.newClient(options), options));
      sync.firstLoad();
      assertEquals(0, core.rulesetHold.remainingNanos());
      Thread loop = new SdkThread(sync::run, "toggleslash-sync");
      loop.start();
      Support.await("the stream hold", 5000, () -> core.rulesetHold.remainingNanos() > 0);
      assertEquals(CAP_NANOS, core.rulesetHold.remainingNanos());
      assertEquals("http_429", core.state.get().lastError());
      // The loop is waiting the hold out, and a second stream request does not come.
      Thread.sleep(300);
      assertEquals(1, server.requests("/sdk/v1/stream").size());
      core.closing.set(true);
      core.closed.countDown();
      loop.join(5000);
      assertFalse(loop.isAlive());
    }
  }

  @Test
  void theEventsRouteHoldsForTheCapAndKeepsTheCounts() throws Exception {
    try (FakeServer server = tooMany("100000")) {
      java.util.List<Throwable> errors = new java.util.concurrent.CopyOnWriteArrayList<>();
      Options options =
          Support.options(server).pacer(new Support.TestPacer(false)).onError(errors::add).build();
      Core core = new Core(options);
      Usage usage = new Usage(core, new Http(Http.newClient(options), options));
      usage.record("f", 1);
      usage.record("g", 0);
      usage.flush(false);
      assertEquals(CAP_NANOS, core.usageHold.remainingNanos());
      assertEquals(0, core.rulesetHold.remainingNanos());
      // Held: a second flush sends nothing.
      usage.flush(false);
      assertEquals(1, server.requests("/sdk/v1/events").size());
      // The final flush does not wait the hold out; it reports the counts it loses.
      errors.clear();
      usage.flush(true);
      assertEquals(1, server.requests("/sdk/v1/events").size());
      assertEquals(1, errors.size());
      assertEquals(
          "closed with 2 usage counts unsent: the server asked to wait",
          errors.get(0).getMessage());
    }
  }
}
