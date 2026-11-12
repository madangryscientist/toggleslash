package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import java.time.Duration;
import java.util.Map;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** {@code variation} does no I/O: it returns while a ruleset request or a usage flush hangs. */
@Timeout(60)
final class HangTest {
  private static final Map<String, Object> CTX = Map.of("kind", "user", "key", "u1");

  private FakeServer server;

  @BeforeEach
  void start() {
    server = new FakeServer();
  }

  @AfterEach
  void stop() {
    server.close();
  }

  @Test
  void variationReturnsWithinABoundWhileTheRulesetRequestHangs() {
    server.ruleset = server.hang();
    Client client =
        Client.create(Support.options(server).requestTimeout(Duration.ofSeconds(4)).build());
    Support.until("the request arrived", 5000, () -> !server.to("/sdk/v1/ruleset").isEmpty());
    long start = System.nanoTime();
    for (int i = 0; i < 1000; i++) {
      Detail<Boolean> detail = client.booleanVariationDetail("checkout", CTX, false);
      assertEquals(Reason.error(ErrorCode.NOT_READY), detail.reason());
      client.status();
    }
    long millis = (System.nanoTime() - start) / 1_000_000L;
    assertTrue(millis < 1000, "1000 calls took " + millis + " ms while the request hung");
    long closeStart = System.nanoTime();
    client.close();
    assertTrue((System.nanoTime() - closeStart) / 1_000_000L < 3000, "close does not wait it out");
    assertFalse(client.ready());
  }

  @Test
  void variationReturnsWithinABoundWhileAUsageFlushHangs() {
    server.ruleset =
        FakeServer.serving("\"v1\"", Support.ruleset(Support.boolFlag("checkout", true, 1)));
    server.events = server.hang();
    Client client =
        Client.create(
            Support.options(server).stream(false)
                .flushInterval(Duration.ofMillis(50))
                .requestTimeout(Duration.ofSeconds(2))
                .build());
    assertTrue(client.ready());
    client.booleanVariation("checkout", CTX, false);
    Support.until("the flush started", 5000, () -> !server.to("/sdk/v1/events").isEmpty());
    long start = System.nanoTime();
    for (int i = 0; i < 1000; i++) {
      assertTrue(client.booleanVariation("checkout", CTX, false));
      client.status();
    }
    long millis = (System.nanoTime() - start) / 1_000_000L;
    assertTrue(millis < 1000, "1000 calls took " + millis + " ms while the flush hung");
    long closeStart = System.nanoTime();
    client.close();
    long closeMillis = (System.nanoTime() - closeStart) / 1_000_000L;
    assertTrue(closeMillis < 2 * 2000 + 1500, "close took " + closeMillis + " ms");
  }
}
