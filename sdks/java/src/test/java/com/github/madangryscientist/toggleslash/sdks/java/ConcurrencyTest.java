package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.File;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

/** Threads, hangs and callbacks that call back: the guarantees of contract section 6. */
class ConcurrencyTest {

  private final FakeServer server = new FakeServer();
  private final List<ToggleSlashClient> clients = new CopyOnWriteArrayList<>();
  private final List<Throwable> errors = new CopyOnWriteArrayList<>();
  private final ExecutorService pool = Executors.newFixedThreadPool(24);

  @AfterEach
  void tearDown() {
    pool.shutdownNow();
    for (ToggleSlashClient client : clients) {
      client.close();
    }
    server.close();
  }

  private ToggleSlashClient start(ClientOptions.Builder builder) {
    ToggleSlashClient client = ToggleSlashClient.create(builder.build());
    clients.add(client);
    return client;
  }

  private ClientOptions.Builder options() {
    return ClientOptions.builder(server.url(), FakeServer.KEY).stream(false)
        .onError(errors::add)
        .requestTimeout(Duration.ofMillis(800));
  }

  /** A server whose every answer is a new ruleset: flag "f" at version n, tagged "vn". */
  private AtomicLong swapping() {
    AtomicLong counter = new AtomicLong();
    server.rulesetHandler =
        x -> {
          long n = counter.incrementAndGet();
          x.getResponseHeaders().add("ETag", "\"v" + n + "\"");
          FakeServer.reply(x, 200, FakeServer.rulesetOf(FakeServer.flag("f", "true", n)));
        };
    return counter;
  }

  @Test
  void manyThreadsOverAllOperationsWhileTheRulesetSwapsKeepEveryEventPaired() throws Exception {
    AtomicLong swaps = swapping();
    AtomicInteger events = new AtomicInteger();
    AtomicInteger unpaired = new AtomicInteger();
    ToggleSlashClient client =
        start(
            options()
                .pollInterval(Duration.ofMillis(2))
                .flushInterval(Duration.ofMillis(10))
                .afterEvaluation(event -> {
                  events.incrementAndGet();
                  if (event.etag().isPresent() && event.flagVersion().isPresent()) {
                    long version = event.flagVersion().getAsLong();
                    if (!event.etag().get().equals("\"v" + version + "\"")) {
                      unpaired.incrementAndGet();
                    }
                  }
                }));
    assertTrue(client.ready());
    int threads = 16;
    long end = System.nanoTime() + 1_500_000_000L;
    AtomicLong calls = new AtomicLong();
    List<Future<?>> done = new ArrayList<>();
    for (int t = 0; t < threads; t++) {
      int id = t;
      done.add(
          pool.submit(() -> {
            Map<String, Object> ctx = Map.of("key", "user-" + id);
            for (int i = 0; System.nanoTime() < end; i++) {
              calls.incrementAndGet();
              switch ((i + id) % 6) {
                case 0 -> client.boolVariation("f", ctx, false);
                case 1 -> client.stringVariation("f", ctx, "x");
                case 2 -> client.intVariationDetail("f", ctx, 1);
                case 3 -> client.jsonVariation("f", ctx, null);
                case 4 -> client.status();
                default -> client.doubleVariation("missing", ctx, 0.5);
              }
            }
          }));
    }
    for (Future<?> f : done) {
      f.get(60, TimeUnit.SECONDS);
    }
    assertEquals(0, unpaired.get());
    assertTrue(calls.get() > 1000);
    assertTrue(events.get() > 1000);
    assertTrue(swaps.get() > 3, "the ruleset swapped " + swaps.get() + " times");
    assertTrue(client.ready());
  }

  @Test
  void closeRacingEvaluationAndTwoClosesRacingFinishWithinTheBound() throws Exception {
    swapping();
    ToggleSlashClient client = start(options().pollInterval(Duration.ofMillis(2)));
    assertTrue(client.ready());
    AtomicBoolean stop = new AtomicBoolean();
    List<Future<?>> workers = new ArrayList<>();
    for (int t = 0; t < 8; t++) {
      workers.add(
          pool.submit(() -> {
            while (!stop.get()) {
              client.boolVariation("f", Map.of("key", "u"), false);
              client.status();
            }
          }));
    }
    Thread.sleep(100);
    CountDownLatch go = new CountDownLatch(1);
    List<Future<?>> closers = new ArrayList<>();
    for (int i = 0; i < 2; i++) {
      closers.add(
          pool.submit(() -> {
            go.await();
            client.close();
            return null;
          }));
    }
    long start = System.nanoTime();
    go.countDown();
    for (Future<?> f : closers) {
      f.get(10, TimeUnit.SECONDS);
    }
    assertTrue(System.nanoTime() - start < 3_000_000_000L);
    stop.set(true);
    for (Future<?> f : workers) {
      f.get(10, TimeUnit.SECONDS);
    }
    assertEquals(StreamState.CLOSED, client.status().stream());
    assertTrue(client.boolVariation("f", Map.of("key", "u"), false));
  }

  @Test
  void evaluationDoesNotWaitForAHungRulesetRequest() throws Exception {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    CountDownLatch hold = new CountDownLatch(1);
    FakeServer.Handler normal = server.rulesetHandler;
    AtomicInteger calls = new AtomicInteger();
    server.rulesetHandler =
        x -> {
          if (calls.incrementAndGet() == 1) {
            normal.handle(x);
          } else {
            hold.await(10, TimeUnit.SECONDS);
          }
        };
    ToggleSlashClient client =
        start(options().pollInterval(Duration.ofMillis(20)).requestTimeout(Duration.ofSeconds(8)));
    assertTrue(client.ready());
    ClientTest.waitFor("a hung poll", 5000, () -> calls.get() >= 2);
    long start = System.nanoTime();
    for (int i = 0; i < 2000; i++) {
      assertTrue(client.boolVariation("f", Map.of("key", "u"), false));
      client.status();
    }
    assertTrue(System.nanoTime() - start < 2_000_000_000L);
    long closing = System.nanoTime();
    client.close();
    assertTrue(System.nanoTime() - closing < 3_000_000_000L);
    hold.countDown();
  }

  @Test
  void evaluationDoesNotWaitForAHungUsageFlushAndCloseIsBounded() throws Exception {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    CountDownLatch hold = new CountDownLatch(1);
    server.eventsHandler = x -> hold.await(10, TimeUnit.SECONDS);
    ToggleSlashClient client =
        start(
            options().flushInterval(Duration.ofMillis(20)).requestTimeout(Duration.ofMillis(600)));
    assertTrue(client.ready());
    client.boolVariation("f", Map.of("key", "u"), false);
    ClientTest.waitFor("a hung flush", 5000, () -> !server.events.isEmpty());
    long start = System.nanoTime();
    for (int i = 0; i < 2000; i++) {
      client.boolVariation("f", Map.of("key", "u"), false);
      client.status();
    }
    assertTrue(System.nanoTime() - start < 2_000_000_000L);
    long closing = System.nanoTime();
    client.close();
    long took = System.nanoTime() - closing;
    assertTrue(took < 2_500_000_000L, "close took " + TimeUnit.NANOSECONDS.toMillis(took) + " ms");
    hold.countDown();
  }

  @Test
  void callbacksMayCallBackIntoTheClientWithoutDeadlock() throws Exception {
    server.rulesetHandler = x -> FakeServer.reply(x, 500, "no");
    AtomicBoolean[] ran = {new AtomicBoolean(), new AtomicBoolean()};
    ToggleSlashClient[] holder = new ToggleSlashClient[1];
    CountDownLatch seen = new CountDownLatch(1);
    ClientOptions.Builder builder =
        ClientOptions.builder(server.url(), FakeServer.KEY).stream(false)
            .requestTimeout(Duration.ofSeconds(5))
            .onError(e -> {
              ToggleSlashClient self = holder[0];
              if (self != null && !ran[0].getAndSet(true)) {
                self.status();
                self.ready();
                self.boolVariation("f", Map.of("key", "u"), false);
                self.close();
                self.close();
                ran[1].set(true);
                seen.countDown();
              }
            });
    holder[0] = start(builder);
    assertTrue(seen.await(8, TimeUnit.SECONDS));
    assertTrue(ran[1].get());
    assertEquals(StreamState.CLOSED, holder[0].status().stream());
    assertFalse(holder[0].ready());
  }

  @Test
  void aHookMayReadStatusAndAskReadyOnTheCallersThread() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    List<Status> statuses = new CopyOnWriteArrayList<>();
    ToggleSlashClient[] holder = new ToggleSlashClient[1];
    holder[0] =
        start(
            options()
                .afterEvaluation(event -> {
                  statuses.add(holder[0].status());
                  holder[0].ready();
                }));
    assertTrue(holder[0].ready());
    holder[0].boolVariation("f", Map.of("key", "u"), false);
    assertEquals(1, statuses.size());
    assertEquals("\"v1\"", statuses.get(0).etag().orElseThrow());
  }

  @Test
  void theClientsThreadsAreDaemons() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    ToggleSlashClient client = start(options().stream(true));
    assertTrue(client.ready());
    int found = 0;
    for (Thread thread : Thread.getAllStackTraces().keySet()) {
      if (thread.getName().startsWith("toggleslash-")) {
        assertTrue(thread.isDaemon(), thread.getName());
        found++;
      }
    }
    assertTrue(found >= 2);
  }

  @Test
  void aProcessWithAClientThatWasNeverClosedStillExits() throws Exception {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    String java = new File(System.getProperty("java.home"), "bin/java").getPath();
    Process process =
        new ProcessBuilder(
                java,
                "-cp",
                System.getProperty("java.class.path"),
                NeverClosed.class.getName(),
                server.url())
            .redirectErrorStream(true)
            .start();
    process.getInputStream().readAllBytes();
    assertTrue(process.waitFor(20, TimeUnit.SECONDS), "the process did not exit");
    assertEquals(0, process.exitValue());
    process.destroyForcibly();
  }

  /** The child process of the test above: it creates a streaming client and just returns. */
  public static final class NeverClosed {
    private NeverClosed() {}

    public static void main(String[] args) {
      ToggleSlashClient client =
          ToggleSlashClient.create(ClientOptions.builder(args[0], FakeServer.KEY).build());
      client.ready();
      System.out.println(client.boolVariation("f", Map.of("key", "u"), false));
    }
  }
}
