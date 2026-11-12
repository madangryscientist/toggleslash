package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** A reader sees one ruleset or the other: a hook's etag and flagVersion belong together. */
@Timeout(60)
final class SwapTest {
  private static final Map<String, Object> CTX = Map.of("kind", "user", "key", "u1");

  @Test
  void aHooksEtagAndFlagVersionBelongToTheSameRulesetUnderConcurrentSwaps() throws Exception {
    AtomicInteger version = new AtomicInteger(1);
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          (exchange, request) -> {
            int n = version.get();
            FakeServer.reply(
                exchange,
                200,
                Support.ruleset(Support.boolFlag("f", true, n)),
                "ETag",
                "\"v" + n + "\"");
          };
      List<EvaluationEvent> events = Collections.synchronizedList(new ArrayList<>());
      Client client =
          Client.create(
              Support.options(server).stream(false)
                  .pollInterval(Duration.ofMillis(5))
                  .afterEvaluation(event -> {
                    if (events.size() < 200_000) {
                      events.add(event);
                    }
                  })
                  .build());
      assertTrue(client.ready());
      AtomicBoolean stop = new AtomicBoolean();
      Thread bumper =
          new Thread(() -> {
            while (!stop.get()) {
              version.incrementAndGet();
              try {
                Thread.sleep(2);
              } catch (InterruptedException e) {
                return;
              }
            }
          });
      bumper.setDaemon(true);
      bumper.start();
      Thread[] readers = new Thread[4];
      for (int i = 0; i < readers.length; i++) {
        readers[i] =
            new Thread(() -> {
              while (!stop.get()) {
                client.booleanVariation("f", CTX, false);
              }
            });
        readers[i].setDaemon(true);
        readers[i].start();
      }
      Support.until(
          "the ruleset swapped many times",
          30_000,
          () -> {
            Status status = client.status();
            return status.etag() != null
                && !status.etag().equals("\"v1\"")
                && version.get() > 60
                && events.size() > 2000;
          });
      stop.set(true);
      bumper.join(5000);
      for (Thread reader : readers) {
        reader.join(5000);
      }
      client.close();
      java.util.Set<String> tags = new java.util.HashSet<>();
      synchronized (events) {
        for (EvaluationEvent event : events) {
          assertTrue(event.etag() != null, "a ruleset is held after ready");
          tags.add(event.etag());
          assertEquals("\"v" + event.flagVersion() + "\"", event.etag());
        }
      }
      assertTrue(tags.size() > 5, "readers saw " + tags.size() + " different rulesets");
    }
  }
}
