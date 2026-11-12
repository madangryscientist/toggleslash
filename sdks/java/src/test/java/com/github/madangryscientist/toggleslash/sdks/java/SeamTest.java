package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyStore;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.TimeUnit;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManagerFactory;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

/** The time-bound parts of the contract, reached through the package's test seams. */
class SeamTest {

  private final FakeServer server = new FakeServer();
  private final List<ToggleSlashClient> clients = new CopyOnWriteArrayList<>();
  private final List<Throwable> errors = new CopyOnWriteArrayList<>();

  @AfterEach
  void tearDown() {
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

  private ClientOptions.Builder options(boolean stream) {
    return ClientOptions.builder(server.url(), FakeServer.KEY).stream(stream)
        .onError(errors::add)
        .jitter(() -> 0.5)
        .requestTimeout(Duration.ofSeconds(5));
  }

  private static void assertAbout300s(long nanos) {
    long seconds = TimeUnit.NANOSECONDS.toSeconds(nanos);
    assertTrue(seconds >= 299 && seconds <= 301, "waited " + seconds + " s");
  }

  @Test
  void aRetryAfterAbove300HoldsTheRulesetRouteFor300Seconds() {
    server.rulesetHandler =
        x -> {
          x.getResponseHeaders().add("Retry-After", "99999");
          FakeServer.reply(x, 429, "slow");
        };
    FakeClock clock = new FakeClock(true, false);
    ToggleSlashClient client = start(options(false).clock(clock));
    assertFalse(client.ready());
    assertEquals("http_429", client.status().lastError().orElseThrow());
    ClientTest.waitFor("the first wait", 5000, () -> !clock.syncWaits.isEmpty());
    assertAbout300s(clock.syncWaits.get(0));
  }

  @Test
  void aRetryAfterAbove300HoldsTheStreamRouteFor300Seconds() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    server.streamHandler =
        x -> {
          x.getResponseHeaders().add("Retry-After", "99999");
          FakeServer.reply(x, 429, "slow");
        };
    FakeClock clock = new FakeClock(true, false);
    ToggleSlashClient client = start(options(true).clock(clock));
    assertTrue(client.ready());
    ClientTest.waitFor("the first wait", 5000, () -> !clock.syncWaits.isEmpty());
    assertAbout300s(clock.syncWaits.get(0));
    ClientTest.waitFor(
        "the 429 reported",
        5000,
        () ->
            errors.stream()
                .anyMatch(e -> e instanceof ToggleSlashException t && t.code().equals("http_429")));
  }

  @Test
  void aRetryAfterAbove300HoldsTheEventsRouteSeparately() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("a", "true", 1)), "\"v1\"");
    server.eventsHandler =
        x -> {
          x.getResponseHeaders().add("Retry-After", "99999");
          FakeServer.reply(x, 429, "slow");
        };
    FakeClock clock = new FakeClock(false, true);
    ToggleSlashClient client =
        start(options(false).clock(clock).flushInterval(Duration.ofSeconds(10)));
    assertTrue(client.ready());
    client.boolVariation("a", Map.of("key", "u"), false);
    ClientTest.waitFor("the first events request", 5000, () -> server.events.size() >= 1);
    // 300 s is thirty 10-second flush intervals: a request before that would be a broken hold
    ClientTest.waitFor("many flush cycles", 8000, () -> clock.flushWaits.size() >= 25);
    assertEquals(1, server.events.size());
    ClientTest.waitFor("the second request", 10000, () -> server.events.size() >= 2);
    assertTrue(clock.flushWaits.size() >= 29, "waits: " + clock.flushWaits.size());
    // the ruleset route was not held by it
    assertTrue(
        client.status().lastError().isEmpty()
            || !client.status().lastError().get().equals("http_429"));
  }

  @Test
  void aStreamQuietForTheWatchdogWindowIsDroppedAsTimeout() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    ToggleSlashClient client = start(options(true).watchdog(Duration.ofMillis(400)));
    assertTrue(client.ready());
    ClientTest.waitFor("connected", 5000, () -> client.status().stream() == StreamState.CONNECTED);
    // one status() read is one snapshot; a second read could already see the reconnect
    ClientTest.waitFor(
        "timeout while disconnected",
        5000,
        () -> {
          Status status = client.status();
          return "timeout".equals(status.lastError().orElse(""))
              && status.stream() == StreamState.DISCONNECTED;
        });
    ClientTest.waitFor("a new stream", 8000, () -> server.stream.size() >= 2);
  }

  @Test
  void aKeepAliveRestartsTheWatchdog() throws Exception {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    ToggleSlashClient client = start(options(true).watchdog(Duration.ofMillis(500)));
    assertTrue(client.ready());
    ClientTest.waitFor("connected", 5000, () -> client.status().stream() == StreamState.CONNECTED);
    long end = System.nanoTime() + 2_000_000_000L;
    while (System.nanoTime() < end) {
      server.push(": keep-alive\n\n");
      Thread.sleep(100);
      assertEquals(StreamState.CONNECTED, client.status().stream());
      assertTrue(client.status().lastError().isEmpty());
    }
    assertEquals(1, server.stream.size());
  }

  @Test
  void aStreamOpenThatNeverSendsHeadersIsATimeout() {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
    server.streamHandler =
        x -> {
          try {
            Thread.sleep(3000);
          } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
          }
        };
    ToggleSlashClient client = start(options(true).requestTimeout(Duration.ofMillis(300)));
    assertTrue(client.ready());
    ClientTest.waitFor(
        "timeout", 5000, () -> "timeout".equals(client.status().lastError().orElse("")));
  }

  @Test
  void anHttpsUrlReachesTheTlsPathWithALocalCertificate(@TempDir Path dir) throws Exception {
    SSLContext tls = localTls(dir);
    try (FakeServer secure = new FakeServer(tls)) {
      secure.serve(FakeServer.rulesetOf(FakeServer.flag("f", "true", 1)), "\"v1\"");
      assertTrue(secure.url().startsWith("https://"));
      ToggleSlashClient trusted =
          ToggleSlashClient.create(
              ClientOptions.builder(secure.url(), FakeServer.KEY).stream(false)
                  .sslContext(tls)
                  .requestTimeout(Duration.ofSeconds(5))
                  .build());
      clients.add(trusted);
      assertTrue(trusted.ready());
      assertTrue(trusted.boolVariation("f", Map.of("key", "u"), false));
      assertEquals("Bearer " + FakeServer.KEY, secure.ruleset.get(0).header("Authorization"));
      // without that certificate trusted the handshake fails, which is a network error, not a crash
      ToggleSlashClient strict =
          ToggleSlashClient.create(
              ClientOptions.builder(secure.url(), FakeServer.KEY).stream(false)
                  .requestTimeout(Duration.ofSeconds(5))
                  .build());
      clients.add(strict);
      assertFalse(strict.ready());
      assertEquals("network", strict.status().lastError().orElseThrow());
    }
  }

  /**
   * A self-signed certificate for 127.0.0.1, made with the JDK's own keytool, used as both ends.
   */
  static SSLContext localTls(Path dir) throws Exception {
    Path store = dir.resolve("local.p12");
    Path keytool = Path.of(System.getProperty("java.home"), "bin", "keytool");
    Process process =
        new ProcessBuilder(
                keytool.toString(),
                "-genkeypair",
                "-alias",
                "local",
                "-keyalg",
                "RSA",
                "-keysize",
                "2048",
                "-validity",
                "2",
                "-dname",
                "CN=localhost",
                "-ext",
                "SAN=ip:127.0.0.1,dns:localhost",
                "-storetype",
                "PKCS12",
                "-keystore",
                store.toString(),
                "-storepass",
                "test-only",
                "-keypass",
                "test-only")
            .redirectErrorStream(true)
            .start();
    process.getInputStream().readAllBytes();
    if (process.waitFor() != 0) {
      throw new IOException("keytool failed");
    }
    KeyStore keys = KeyStore.getInstance("PKCS12");
    try (var in = Files.newInputStream(store)) {
      keys.load(in, "test-only".toCharArray());
    }
    KeyManagerFactory km = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
    km.init(keys, "test-only".toCharArray());
    TrustManagerFactory tm =
        TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
    tm.init(keys);
    SSLContext context = SSLContext.getInstance("TLS");
    context.init(km.getKeyManagers(), tm.getTrustManagers(), null);
    return context;
  }

  @Test
  void eachRouteSendsOnlyTheHeadersThePrivacyRuleAllows() throws Exception {
    server.serve(FakeServer.rulesetOf(FakeServer.flag("a", "true", 1)), "\"v1\"");
    ToggleSlashClient client =
        start(
            options(true)
                .pollInterval(Duration.ofMillis(100))
                .flushInterval(Duration.ofMillis(50)));
    assertTrue(client.ready());
    ClientTest.waitFor("connected", 5000, () -> client.status().stream() == StreamState.CONNECTED);
    String key = "end-user-key-12345";
    client.boolVariation("a", Map.of("key", key, "attributes", Map.of("email", "x@y.z")), false);
    ClientTest.waitFor("usage", 5000, () -> !server.events.isEmpty());
    // a conditional ruleset request needs a held tag, so ask again through the stream
    server.push("event: changed\ndata: \"other\"\n\n");
    ClientTest.waitFor("a conditional fetch", 5000, () -> server.ruleset.size() >= 2);
    StringBuilder table = new StringBuilder();
    for (Map.Entry<String, List<FakeServer.Seen>> route :
        Map.of("ruleset", server.ruleset, "stream", server.stream, "events", server.events)
            .entrySet()) {
      for (FakeServer.Seen seen : route.getValue()) {
        for (Map.Entry<String, List<String>> header : seen.headers().entrySet()) {
          String name = header.getKey().toLowerCase();
          assertFalse(String.join(",", header.getValue()).contains(key), name);
          assertFalse(String.join(",", header.getValue()).contains("x@y.z"), name);
          assertTrue(
              List.of(
                      "authorization",
                      "accept",
                      "if-none-match",
                      "content-type",
                      "host",
                      "user-agent",
                      "content-length",
                      "connection")
                  .contains(name),
              "unexpected header " + name + " on " + route.getKey());
        }
        assertFalse(seen.text().contains(key));
        assertFalse(seen.text().contains("x@y.z"));
        table.append(route.getKey()).append(' ').append(seen.method()).append(' ');
        seen.headers().keySet().stream()
            .map(String::toLowerCase)
            .sorted()
            .forEach(h -> table.append(h).append(' '));
        table.append('\n');
      }
    }
    Path out = Path.of(System.getProperty("toggleslash.headers", "target/headers.txt"));
    Files.createDirectories(out.toAbsolutePath().getParent());
    Files.writeString(out, table.toString(), StandardCharsets.UTF_8);
    assertTrue(server.ruleset.stream().anyMatch(s -> s.header("If-None-Match") != null));
  }
}
