package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.FakeServer.Req;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.ReasonKind;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.io.IOException;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyStore;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.TreeSet;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManagerFactory;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

/** The client against a loopback server: headers, bounds, races, TLS and the shutdown order. */
class ClientTest {
  private static final String KEY = "sdk-key-1";

  private static Options.Builder options(FakeServer server) {
    return Options.builder(server.url(), KEY).stream(false).requestTimeout(Duration.ofSeconds(5));
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> object(Object value) {
    return (Map<String, Object>) value;
  }

  @SuppressWarnings("unchecked")
  private static List<Object> array(Object value) {
    return (List<Object>) value;
  }

  private static Object parse(String text) {
    byte[] bytes = text.getBytes(StandardCharsets.UTF_8);
    return Json.parse(bytes, 0, bytes.length);
  }

  private static FakeServer servingOne() throws IOException {
    FakeServer server = new FakeServer();
    server.route("/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.ruleset(1, 1)));
    server.route("/sdk/v1/events", (req, ex) -> FakeServer.reply(ex, 200, Map.of(), "{}"));
    return server;
  }

  @Test
  void loadsServesAndCounts() throws Exception {
    try (FakeServer server = servingOne()) {
      try (Client client = Client.create(options(server).build())) {
        assertTrue(client.ready());
        assertEquals(Boolean.TRUE, client.boolVariation("f", Map.of("key", "u"), false));
        Status status = client.status();
        assertEquals("e1", status.etag());
        assertNotNull(status.lastSyncAt());
        assertEquals(StreamState.OFF, status.stream());
        assertNull(status.lastError());
      }
      List<Req> events = server.requests("/sdk/v1/events");
      assertEquals(1, events.size());
      Map<String, Object> entry =
          object(array(object(parse(events.get(0).text())).get("events")).get(0));
      assertEquals("f", entry.get("flag"));
      assertEquals(1.0, entry.get("variationIndex"));
      assertEquals(1.0, entry.get("count"));
      assertTrue(entry.get("day").toString().matches("\\d{4}-\\d{2}-\\d{2}"));
    }
  }

  /** Records which headers each route got, and writes the audit table the run report quotes. */
  @Test
  void headerAudit() throws Exception {
    String secret = "secret-user-4f9a";
    try (FakeServer server = new FakeServer()) {
      server.route("/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.ruleset(1, 1)));
      server.route("/sdk/v1/stream", (req, ex) -> FakeServer.reply(ex, 500, Map.of(), "no"));
      server.route("/sdk/v1/events", (req, ex) -> FakeServer.reply(ex, 200, Map.of(), "{}"));
      List<Throwable> errors = new CopyOnWriteArrayList<>();
      Client client =
          Client.create(
              Options.builder(server.url(), KEY)
                  .pollInterval(Duration.ofMillis(60))
                  .flushInterval(Duration.ofMillis(60))
                  .onError(errors::add)
                  .build());
      assertTrue(client.ready());
      assertTrue(
          client.boolVariation("f", Map.of("key", secret, "email", secret + "@x.test"), false));
      assertTrue(
          Support.waitFor(
              () ->
                  server.requests("/sdk/v1/ruleset").size() >= 2
                      && !server.requests("/sdk/v1/events").isEmpty()
                      && !server.requests("/sdk/v1/stream").isEmpty(),
              5000));
      Status status = client.status();
      client.close();

      Req plain = server.requests("/sdk/v1/ruleset").get(0);
      Req conditional = server.requests("/sdk/v1/ruleset").get(1);
      Req stream = server.requests("/sdk/v1/stream").get(0);
      Req events = server.requests("/sdk/v1/events").get(0);
      Map<String, Map<String, String>> routes = new LinkedHashMap<>();
      routes.put("GET /sdk/v1/ruleset", plain.headers());
      routes.put("GET /sdk/v1/ruleset (conditional)", conditional.headers());
      routes.put("GET /sdk/v1/stream", stream.headers());
      routes.put("POST /sdk/v1/events", events.headers());
      Map<String, Set<String>> mine = new LinkedHashMap<>();
      mine.put("GET /sdk/v1/ruleset", Set.of("authorization"));
      mine.put("GET /sdk/v1/ruleset (conditional)", Set.of("authorization", "if-none-match"));
      mine.put("GET /sdk/v1/stream", Set.of("authorization", "accept"));
      mine.put("POST /sdk/v1/events", Set.of("authorization", "content-type"));
      Set<String> platform = Set.of("host", "user-agent", "content-length", "connection");

      StringBuilder table =
          new StringBuilder(
              "| Route | Set by the SDK | Set by the HttpClient |\n| --- | --- | --- |\n");
      for (Map.Entry<String, Map<String, String>> route : routes.entrySet()) {
        Set<String> names = new TreeSet<>(route.getValue().keySet());
        Set<String> sdk = new TreeSet<>(mine.get(route.getKey()));
        Set<String> own = new TreeSet<>(names);
        own.removeAll(sdk);
        assertTrue(names.containsAll(sdk), route.getKey() + " lacks " + sdk + " in " + names);
        assertTrue(platform.containsAll(own), route.getKey() + " has the unexpected " + own);
        table
            .append("| ")
            .append(route.getKey())
            .append(" | ")
            .append(String.join(", ", sdk))
            .append(" | ")
            .append(String.join(", ", own))
            .append(" |\n");
        assertEquals("Bearer " + KEY, route.getValue().get("authorization"));
      }
      assertEquals("e1", conditional.headers().get("if-none-match"));
      assertFalse(plain.headers().containsKey("if-none-match"));
      assertEquals("text/event-stream", stream.headers().get("accept"));
      assertEquals("application/json", events.headers().get("content-type"));
      Files.createDirectories(Path.of("target"));
      Files.writeString(Path.of("target/header-audit.txt"), table.toString());

      // Privacy: nothing the application passed in reaches a request, a status or an error.
      List<Req> all = new ArrayList<>();
      for (String path : List.of("/sdk/v1/ruleset", "/sdk/v1/stream", "/sdk/v1/events")) {
        all.addAll(server.requests(path));
      }
      for (Req req : all) {
        assertFalse(req.text().contains(secret));
        assertFalse(req.path().contains(secret));
        assertFalse(req.headers().toString().contains(secret));
      }
      assertFalse(status.toString().contains(secret));
      for (Throwable error : errors) {
        assertFalse(String.valueOf(error.getMessage()).contains(secret));
      }
      String body = events.text();
      assertTrue(
          body.matches(
              "\\{\"events\":\\[\\{\"flag\":\"f\",\"variationIndex\":1,\"count\":1,\"day\":\"\\d{4}-\\d{2}-\\d{2}\"\\}\\]\\}"),
          body);
    }
  }

  @Test
  void conditionalRequestIsASync() throws Exception {
    try (FakeServer server = servingOne()) {
      try (Client client =
          Client.create(options(server).pollInterval(Duration.ofMillis(40)).build())) {
        assertTrue(client.ready());
        Long first =
            client.status().lastSyncAt() == null
                ? null
                : client.status().lastSyncAt().toEpochMilli();
        assertNotNull(first);
        assertTrue(
            Support.waitFor(() -> client.status().lastSyncAt().toEpochMilli() > first, 3000));
        assertEquals("e1", client.status().etag());
        assertNull(client.status().lastError());
        assertEquals(Boolean.TRUE, client.boolVariation("f", Map.of("key", "u"), false));
      }
    }
  }

  @Test
  void tlsWithALocalCertificate(@TempDir Path dir) throws Exception {
    Path store = dir.resolve("store.p12");
    Process keytool =
        new ProcessBuilder(
                Path.of(System.getProperty("java.home"), "bin", "keytool").toString(),
                "-genkeypair",
                "-alias",
                "local",
                "-keyalg",
                "RSA",
                "-keysize",
                "2048",
                "-validity",
                "2",
                "-storetype",
                "PKCS12",
                "-keystore",
                store.toString(),
                "-storepass",
                "changeit",
                "-dname",
                "CN=localhost",
                "-ext",
                "san=ip:127.0.0.1,dns:localhost")
            .redirectErrorStream(true)
            .start();
    keytool.getInputStream().readAllBytes();
    assertEquals(0, keytool.waitFor());
    KeyStore keys = KeyStore.getInstance("PKCS12");
    try (var in = Files.newInputStream(store)) {
      keys.load(in, "changeit".toCharArray());
    }
    KeyManagerFactory kmf = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
    kmf.init(keys, "changeit".toCharArray());
    TrustManagerFactory tmf =
        TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
    tmf.init(keys);
    SSLContext serverSide = SSLContext.getInstance("TLS");
    serverSide.init(kmf.getKeyManagers(), null, null);
    SSLContext clientSide = SSLContext.getInstance("TLS");
    clientSide.init(null, tmf.getTrustManagers(), null);

    try (FakeServer server = new FakeServer(serverSide)) {
      server.route("/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.ruleset(1, 1)));
      server.route("/sdk/v1/events", (req, ex) -> FakeServer.reply(ex, 200, Map.of(), "{}"));
      assertTrue(server.url().startsWith("https://"));
      try (Client trusting = Client.create(options(server).sslContext(clientSide).build())) {
        assertTrue(trusting.ready());
        assertEquals(Boolean.TRUE, trusting.boolVariation("f", Map.of("key", "u"), false));
      }
      // Without the seam the platform's trust store does not know this certificate.
      try (Client strict = Client.create(options(server).build())) {
        assertFalse(strict.ready());
        assertEquals(Status.NETWORK, strict.status().lastError());
      }
    }
  }

  @Test
  void retryAfterIsCappedOnTheRulesetRoute() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route(
          "/sdk/v1/ruleset",
          (req, ex) -> FakeServer.reply(ex, 429, Map.of("Retry-After", "100000"), ""));
      try (Client client =
          Client.create(
              options(server)
                  .pollInterval(Duration.ofMillis(20))
                  .retryAfterCap(Duration.ofMillis(400))
                  .build())) {
        assertNotNull(client.status());
        assertTrue(Support.waitFor(() -> server.requests("/sdk/v1/ruleset").size() >= 2, 5000));
        long gap =
            server.requests("/sdk/v1/ruleset").get(1).at()
                - server.requests("/sdk/v1/ruleset").get(0).at();
        assertTrue(gap >= 330 && gap < 3000, "gap " + gap);
      }
    }
  }

  @Test
  void retryAfterIsCappedOnTheStreamRoute() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route("/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.ruleset(1, 1)));
      server.route(
          "/sdk/v1/stream",
          (req, ex) -> FakeServer.reply(ex, 429, Map.of("Retry-After", "100000"), ""));
      try (Client client =
          Client.create(
              Options.builder(server.url(), KEY)
                  .pollInterval(Duration.ofMillis(30))
                  .retryAfterCap(Duration.ofMillis(400))
                  .jitter(() -> 0.0)
                  .build())) {
        assertNotNull(client.status());
        assertTrue(Support.waitFor(() -> server.requests("/sdk/v1/stream").size() >= 2, 5000));
        long gap =
            server.requests("/sdk/v1/stream").get(1).at()
                - server.requests("/sdk/v1/stream").get(0).at();
        assertTrue(gap >= 330 && gap < 3000, "gap " + gap);
      }
    }
  }

  @Test
  void retryAfterIsCappedOnTheEventsRoute() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route("/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.ruleset(1, 1)));
      server.route(
          "/sdk/v1/events",
          (req, ex) -> FakeServer.reply(ex, 429, Map.of("Retry-After", "100000"), ""));
      try (Client client =
          Client.create(
              options(server)
                  .flushInterval(Duration.ofMillis(30))
                  .retryAfterCap(Duration.ofMillis(400))
                  .build())) {
        assertTrue(client.ready());
        client.boolVariation("f", Map.of("key", "u"), false);
        assertTrue(Support.waitFor(() -> server.requests("/sdk/v1/events").size() >= 2, 5000));
        long gap =
            server.requests("/sdk/v1/events").get(1).at()
                - server.requests("/sdk/v1/events").get(0).at();
        assertTrue(gap >= 330 && gap < 3000, "gap " + gap);
      }
    }
  }

  @Test
  void aQuietStreamIsDroppedAfterTheIdleLimitAndAKeepAliveRestartsIt() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route("/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.ruleset(1, 1)));
      AtomicInteger opens = new AtomicInteger();
      server.route(
          "/sdk/v1/stream",
          (req, ex) -> {
            OutputStream out = FakeServer.openStream(ex);
            if (opens.incrementAndGet() > 1) {
              try {
                server.hang();
              } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
              }
              return;
            }
            out.write("event: connected\ndata: e1\n\n".getBytes(StandardCharsets.UTF_8));
            out.flush();
            for (int i = 0; i < 8; i++) {
              Thread.sleep(100);
              out.write(": keep-alive\n\n".getBytes(StandardCharsets.UTF_8));
              out.flush();
            }
            server.hang();
          });
      try (Client client =
          Client.create(
              Options.builder(server.url(), KEY)
                  .pollInterval(Duration.ofSeconds(30))
                  .requestTimeout(Duration.ofSeconds(5))
                  .streamIdle(Duration.ofMillis(300))
                  .build())) {
        assertTrue(Support.waitFor(() -> client.status().stream() == StreamState.CONNECTED, 3000));
        Thread.sleep(600);
        // 600 ms is twice the idle limit, and the keep-alives are 100 ms apart.
        assertEquals(StreamState.CONNECTED, client.status().stream());
        assertNull(client.status().lastError());
        // The keep-alives stop at 800 ms, and 300 ms after that the stream is dropped.
        assertTrue(Support.waitFor(() -> "timeout".equals(client.status().lastError()), 4000));
        assertTrue(Support.waitFor(() -> client.status().stream() != StreamState.CONNECTED, 1000));
      }
    }
  }

  @Test
  void variationStaysBoundedWhileTheRulesetRequestHangs() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route("/sdk/v1/ruleset", (req, ex) -> server.hangFor());
      Client client = Client.create(options(server).requestTimeout(Duration.ofMillis(400)).build());
      long worst = 0;
      for (int i = 0; i < 50; i++) {
        long start = System.nanoTime();
        var detail = client.boolVariationDetail("f", Map.of("key", "u"), false);
        worst = Math.max(worst, (System.nanoTime() - start) / 1_000_000);
        assertEquals(ErrorCode.NOT_READY, detail.reason().code());
      }
      assertTrue(worst < 200, "variation took " + worst + " ms");
      long start = System.nanoTime();
      assertFalse(client.ready());
      long waited = (System.nanoTime() - start) / 1_000_000;
      assertTrue(waited < 2 * 400 + 1500, "ready took " + waited + " ms");
      assertEquals(Status.TIMEOUT, client.status().lastError());
      start = System.nanoTime();
      client.close();
      assertTrue((System.nanoTime() - start) / 1_000_000 < 2 * 400 + 1500);
    }
  }

  @Test
  void variationStaysBoundedWhileAFlushHangs() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route("/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.ruleset(1, 1)));
      server.route("/sdk/v1/events", (req, ex) -> server.hangFor());
      Client client =
          Client.create(
              options(server)
                  .requestTimeout(Duration.ofMillis(500))
                  .flushInterval(Duration.ofMillis(40))
                  .build());
      assertTrue(client.ready());
      long worst = 0;
      long end = System.nanoTime() + 700_000_000L;
      while (System.nanoTime() < end) {
        long start = System.nanoTime();
        assertEquals(Boolean.TRUE, client.boolVariation("f", Map.of("key", "u"), false));
        worst = Math.max(worst, (System.nanoTime() - start) / 1_000_000);
      }
      assertFalse(server.requests("/sdk/v1/events").isEmpty());
      assertTrue(worst < 300, "variation took " + worst + " ms");
      long start = System.nanoTime();
      client.close();
      assertTrue((System.nanoTime() - start) / 1_000_000 < 2 * 500 + 1500);
    }
  }

  /** The hook's tag and flag version always come from one ruleset, however fast it is swapped. */
  @Test
  void hookEtagAndFlagVersionComeFromOneSwap() throws Exception {
    try (FakeServer server = new FakeServer()) {
      AtomicLong served = new AtomicLong();
      server.route(
          "/sdk/v1/ruleset",
          (req, ex) -> {
            long n = served.incrementAndGet();
            FakeServer.reply(ex, 200, Map.of("ETag", "e" + n), FakeServer.ruleset(1, n));
          });
      List<EvaluationEvent> events = new CopyOnWriteArrayList<>();
      Client client =
          Client.create(
              options(server)
                  .pollInterval(Duration.ofMillis(1))
                  .afterEvaluation(events::add)
                  .build());
      assertTrue(client.ready());
      Thread[] threads = new Thread[4];
      long end = System.nanoTime() + 1_500_000_000L;
      for (int t = 0; t < threads.length; t++) {
        threads[t] =
            new Thread(() -> {
              while (System.nanoTime() < end) {
                client.boolVariation("f", Map.of("key", "u"), false);
              }
            });
        threads[t].start();
      }
      for (Thread thread : threads) {
        thread.join();
      }
      client.close();
      Set<String> tags = new TreeSet<>();
      for (EvaluationEvent event : events) {
        assertEquals("e" + event.flagVersion(), event.etag());
        tags.add(event.etag());
      }
      assertTrue(tags.size() > 5, "only " + tags.size() + " distinct rulesets were seen");
    }
  }

  @Test
  void callbacksMayCallBackIntoTheClient() throws Exception {
    try (FakeServer server = servingOne()) {
      AtomicReference<Client> ref = new AtomicReference<>();
      AtomicInteger depth = new AtomicInteger();
      List<Object> nested = new CopyOnWriteArrayList<>();
      Client client =
          Client.create(
              options(server)
                  .afterEvaluation(event -> {
                    if (depth.getAndIncrement() == 0) {
                      nested.add(ref.get().boolVariation("f", Map.of("key", "inner"), false));
                      nested.add(ref.get().status().etag());
                    }
                    depth.decrementAndGet();
                  })
                  .build());
      ref.set(client);
      assertTrue(client.ready());
      assertEquals(Boolean.TRUE, client.boolVariation("f", Map.of("key", "outer"), false));
      assertEquals(List.of(Boolean.TRUE, "e1"), nested);
      client.close();
    }
  }

  @Test
  void readyAndCloseFromTheSdksOwnThreadDoNotWait() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route("/sdk/v1/ruleset", (req, ex) -> server.hangFor());
      Client client = Client.create(options(server).requestTimeout(Duration.ofSeconds(5)).build());
      AtomicReference<Boolean> ready = new AtomicReference<>();
      AtomicLong readyMs = new AtomicLong();
      AtomicLong closeMs = new AtomicLong();
      CountDownLatch done = new CountDownLatch(1);
      SdkThread own =
          new SdkThread(
              client,
              "test-own-thread",
              () -> {
                long start = System.nanoTime();
                ready.set(client.ready());
                readyMs.set((System.nanoTime() - start) / 1_000_000);
                start = System.nanoTime();
                client.close();
                closeMs.set((System.nanoTime() - start) / 1_000_000);
                done.countDown();
              });
      own.start();
      assertTrue(done.await(4, TimeUnit.SECONDS));
      assertEquals(Boolean.FALSE, ready.get());
      assertTrue(readyMs.get() < 500, "ready waited " + readyMs.get());
      assertTrue(closeMs.get() < 500, "close waited " + closeMs.get());
      // Another close, from here, joins the same shutdown.
      client.close();
      assertEquals(StreamState.CLOSED, client.status().stream());
    }
  }

  @Test
  void aFailedFirstLoadReportsToOnErrorOnTheSdkThread() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route("/sdk/v1/ruleset", (req, ex) -> FakeServer.reply(ex, 503, Map.of(), ""));
      AtomicReference<Client> ref = new AtomicReference<>();
      CountDownLatch set = new CountDownLatch(1);
      CountDownLatch called = new CountDownLatch(1);
      AtomicBoolean onSdkThread = new AtomicBoolean();
      AtomicReference<Boolean> readyInside = new AtomicReference<>();
      Client client =
          Client.create(
              options(server)
                  .pollInterval(Duration.ofSeconds(30))
                  .onError(error -> {
                    try {
                      set.await();
                    } catch (InterruptedException e) {
                      Thread.currentThread().interrupt();
                    }
                    onSdkThread.set(SdkThread.isOwnedBy(ref.get()));
                    readyInside.set(ref.get().ready());
                    called.countDown();
                    throw new IllegalStateException("onError throws and the SDK ignores it");
                  })
                  .build());
      ref.set(client);
      set.countDown();
      assertTrue(called.await(5, TimeUnit.SECONDS));
      assertTrue(onSdkThread.get());
      assertEquals(Boolean.FALSE, readyInside.get());
      assertEquals("http_503", client.status().lastError());
      client.close();
    }
  }

  @Test
  void anEvaluatorThatRaisesGivesEvaluationFailedAndOneOnError() throws Exception {
    try (FakeServer server = servingOne()) {
      AtomicInteger errors = new AtomicInteger();
      List<EvaluationEvent> events = new CopyOnWriteArrayList<>();
      try (Client client =
          Client.create(
              options(server)
                  .evaluator((ruleset, key, context) -> {
                    throw new IllegalStateException("evaluator raised");
                  })
                  .onError(e -> errors.incrementAndGet())
                  .afterEvaluation(events::add)
                  .build())) {
        assertTrue(client.ready());
        errors.set(0);
        var detail = client.boolVariationDetail("f", Map.of("key", "u"), false);
        assertEquals(Boolean.FALSE, detail.value());
        assertEquals(ReasonKind.ERROR, detail.reason().kind());
        assertEquals(ErrorCode.EVALUATION_FAILED, detail.reason().code());
        assertTrue(Support.waitFor(() -> errors.get() >= 1, 1000));
        Thread.sleep(100);
        assertEquals(1, errors.get());
        assertEquals(1, events.size());
      }
      assertTrue(server.requests("/sdk/v1/events").isEmpty(), "a failed evaluation is not counted");
    }
  }

  @Test
  void usageSplitsAtUtcMidnight() throws Exception {
    try (FakeServer server = servingOne()) {
      long start = Instant.parse("2026-01-01T23:59:59.999Z").toEpochMilli();
      Support.FakeClock clock = new Support.FakeClock(start);
      Client client =
          Client.create(options(server).clock(clock).flushInterval(Duration.ofHours(1)).build());
      assertTrue(client.ready());
      client.boolVariation("f", Map.of("key", "u"), false);
      clock.set(start + 1);
      client.boolVariation("f", Map.of("key", "u"), false);
      client.boolVariation("f", Map.of("key", "u"), false);
      client.close();
      List<Req> events = server.requests("/sdk/v1/events");
      assertEquals(1, events.size());
      List<Object> entries = array(object(parse(events.get(0).text())).get("events"));
      Map<String, Double> byDay = new TreeMap<>();
      for (Object entry : entries) {
        byDay.put(object(entry).get("day").toString(), (Double) object(entry).get("count"));
      }
      assertEquals(Map.of("2026-01-01", 1.0, "2026-01-02", 2.0), byDay);
    }
  }

  @Test
  void theIntegerAccessorIsStrict() throws Exception {
    String[][] cases = {
      {"2.0", "2"},
      {"9223372036854774784", "9223372036854774784"},
      {"-9223372036854775808", "-9223372036854775808"},
      {"2.5", null},
      {"1e20", null},
      {"9223372036854775808", null},
      {"\"7\"", null},
      {"true", null},
    };
    for (String[] c : cases) {
      try (FakeServer server = new FakeServer()) {
        server.route(
            "/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.valueRuleset(c[0])));
        server.route("/sdk/v1/events", (req, ex) -> FakeServer.reply(ex, 200, Map.of(), "{}"));
        try (Client client = Client.create(options(server).build())) {
          assertTrue(client.ready());
          var detail = client.longVariationDetail("n", Map.of("key", "u"), -1L);
          if (c[1] == null) {
            assertEquals(-1L, detail.value(), c[0]);
            assertEquals(ErrorCode.TYPE_MISMATCH, detail.reason().code(), c[0]);
            assertNull(detail.variationIndex());
          } else {
            assertEquals(Long.parseLong(c[1]), detail.value(), c[0]);
            assertEquals(Integer.valueOf(0), detail.variationIndex());
          }
          // A generic call with an integer fallback is the integer accessor.
          Object generic = client.variation("n", Map.of("key", "u"), -1L);
          assertEquals(c[1] == null ? -1L : Long.parseLong(c[1]), generic, c[0]);
        }
      }
    }
  }

  @Test
  void mismatchRulesFollowTheJsonKinds() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.route(
          "/sdk/v1/ruleset", FakeServer.rulesetRoute("e1", FakeServer.valueRuleset("[1]")));
      server.route("/sdk/v1/events", (req, ex) -> FakeServer.reply(ex, 200, Map.of(), "{}"));
      try (Client client = Client.create(options(server).build())) {
        assertTrue(client.ready());
        Map<String, Object> ctx = Map.of("key", "u");
        assertEquals(
            ErrorCode.TYPE_MISMATCH, client.variationDetail("n", ctx, Map.of()).reason().code());
        assertEquals(
            ErrorCode.TYPE_MISMATCH, client.variationDetail("n", ctx, "x").reason().code());
        assertEquals(
            ReasonKind.FALLTHROUGH, client.variationDetail("n", ctx, List.of()).reason().kind());
        assertEquals(List.of(1.0), client.variationDetail("n", ctx, null).value());
        assertEquals(List.of(1.0), client.jsonVariation("n", ctx, Map.of()));
      }
    }
  }

  @Test
  void aClientNeverClosedDoesNotKeepTheJvmAlive() throws Exception {
    Process probe =
        new ProcessBuilder(
                Path.of(System.getProperty("java.home"), "bin", "java").toString(),
                "-cp",
                Path.of("target/classes").toAbsolutePath()
                    + java.io.File.pathSeparator
                    + Path.of("target/test-classes").toAbsolutePath(),
                DaemonProbe.class.getName())
            .redirectErrorStream(true)
            .start();
    assertTrue(probe.waitFor(15, TimeUnit.SECONDS), "the JVM is still running");
    assertEquals(0, probe.exitValue());
    assertTrue(
        new String(probe.getInputStream().readAllBytes(), StandardCharsets.UTF_8)
            .contains("probe done"));
  }

  @Test
  void aUrlTheSdkCannotUseIsANetworkErrorNotARaise() {
    for (String url : new String[] {"", "not a url", "ftp://127.0.0.1", "http://", null}) {
      try (Client client = Client.create(Options.builder(url, "k").stream(false).build())) {
        assertFalse(client.ready(), String.valueOf(url));
        assertEquals(Status.NETWORK, client.status().lastError(), String.valueOf(url));
        assertEquals(Boolean.FALSE, client.variation("f", Map.of(), Boolean.FALSE));
      }
    }
    assertFalse(Client.create(null).ready());
  }

  @Test
  void trailingSlashesAreRemovedFromTheUrl() throws Exception {
    try (FakeServer server = servingOne()) {
      try (Client client =
          Client.create(Options.builder(server.url() + "///", KEY).stream(false).build())) {
        assertTrue(client.ready());
      }
      assertEquals("/sdk/v1/ruleset", server.requests("/sdk/v1/ruleset").get(0).path());
    }
  }
}
