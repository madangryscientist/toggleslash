package com.github.madangryscientist.toggleslash.sdks.java;

import static com.github.madangryscientist.toggleslash.sdks.java.Support.eventually;
import static com.github.madangryscientist.toggleslash.sdks.java.Support.ms;
import static com.github.madangryscientist.toggleslash.sdks.java.Support.options;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpsConfigurator;
import com.sun.net.httpserver.HttpsServer;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyStore;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.TreeSet;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManagerFactory;
import org.junit.jupiter.api.Test;

/** What goes over the wire: headers, privacy, TLS and the daemon threads. */
class TransportTest {
  private static final String SECRET = "ctx-secret-zebra-42";

  /** The headers the SDK sets, and the ones any HTTP client adds on its own. */
  private static final Set<String> SDK_HEADERS =
      Set.of("authorization", "accept", "if-none-match", "content-type");

  private static final Set<String> CLIENT_DEFAULTS =
      Set.of("host", "user-agent", "content-length", "connection");

  @Test
  void headerAuditAndNoContextOnTheWire() throws Exception {
    try (FakeServer server = new FakeServer()) {
      AtomicInteger rulesets = new AtomicInteger();
      server.ruleset =
          ex -> {
            int n = rulesets.incrementAndGet();
            ex.getResponseHeaders().set("ETag", "\"v" + n + "\"");
            FakeServer.send(ex, 200, FakeServer.RULESET);
          };
      server.stream =
          ex -> {
            OutputStream out = FakeServer.openStream(ex);
            FakeServer.write(out, "event: connected\ndata: \"v1\"\n\n");
            Thread.sleep(150);
            FakeServer.write(out, "event: changed\ndata: \"v2\"\n\n");
            server.released.await();
          };
      List<String> errors = new CopyOnWriteArrayList<>();
      Context context = Context.of("user", "key-" + SECRET, Map.of("email", SECRET + "@x.test"));
      Client client =
          Client.create(
              options(server).stream(true)
                  .flushInterval(ms(30))
                  .onError(e -> errors.add(String.valueOf(e.getMessage()))));
      assertTrue(client.ready());
      eventually("the conditional fetch", 5000, () -> server.to("/sdk/v1/ruleset").size() >= 2);
      client.boolVariation("f", context, false);
      client.jsonVariationDetail("f", context, null);
      eventually("a usage post", 5000, () -> !server.to("/sdk/v1/events").isEmpty());
      client.close();

      Map<String, Set<String>> table = new TreeMap<>();
      Set<String> allowed = new TreeSet<>(SDK_HEADERS);
      allowed.addAll(CLIENT_DEFAULTS);
      for (FakeServer.Seen seen : server.seen) {
        String route = seen.method() + " " + seen.path();
        if (seen.path().equals("/sdk/v1/ruleset") && seen.headers().containsKey("if-none-match")) {
          route += " (conditional)";
        }
        table.computeIfAbsent(route, k -> new TreeSet<>()).addAll(seen.headers().keySet());
        for (String name : seen.headers().keySet()) {
          assertTrue(allowed.contains(name), name + " on " + route);
        }
        assertFalse(seen.body().contains(SECRET), "context in the body of " + route);
        for (List<String> values : seen.headers().values()) {
          for (String value : values) {
            assertFalse(value.contains(SECRET), "context in a header of " + route);
          }
        }
        assertEquals("Bearer fs_server_testkey", seen.headers().get("authorization").get(0));
      }
      assertTrue(table.containsKey("GET /sdk/v1/ruleset"));
      assertTrue(table.containsKey("GET /sdk/v1/ruleset (conditional)"));
      assertTrue(table.containsKey("GET /sdk/v1/stream"));
      assertTrue(table.containsKey("POST /sdk/v1/events"));
      assertTrue(table.get("POST /sdk/v1/events").contains("content-type"));
      for (String error : errors) {
        assertFalse(error.contains(SECRET));
      }
      assertFalse(String.valueOf(client.status()).contains(SECRET));
      StringBuilder out = new StringBuilder();
      table.forEach((route, names) ->
          out.append(route).append(" | ").append(String.join(", ", names)).append('\n'));
      Files.createDirectories(Path.of("target"));
      Files.writeString(Path.of("target", "headers.txt"), out.toString());
    }
  }

  @Test
  void errorsAndStatusNeverCarryAContext() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset = FakeServer.status(500);
      List<String> errors = new CopyOnWriteArrayList<>();
      Client client = Client.create(options(server).onError(e -> errors.add(e.toString())));
      Context context = Context.of("user", SECRET, Map.of("a", SECRET));
      client.boolVariation("f", context, false);
      client.boolVariationDetail("f", context, false);
      eventually("a sync error", 5000, () -> !errors.isEmpty());
      client.close();
      for (String error : errors) {
        assertFalse(error.contains(SECRET));
      }
      assertFalse(String.valueOf(client.status()).contains(SECRET));
    }
  }

  private static Path keystore(Path dir) throws Exception {
    Path store = dir.resolve("tls.p12");
    String keytool = Path.of(System.getProperty("java.home"), "bin", "keytool").toString();
    Process process =
        new ProcessBuilder(
                keytool,
                "-genkeypair",
                "-alias",
                "t",
                "-keyalg",
                "RSA",
                "-keysize",
                "2048",
                "-dname",
                "CN=localhost",
                "-ext",
                "san=ip:127.0.0.1,dns:localhost",
                "-validity",
                "2",
                "-storetype",
                "PKCS12",
                "-keystore",
                store.toString(),
                "-storepass",
                "changeit")
            .redirectErrorStream(true)
            .start();
    process.getInputStream().readAllBytes();
    assertEquals(0, process.waitFor(30, TimeUnit.SECONDS) ? process.exitValue() : -1);
    return store;
  }

  @Test
  void speaksTlsToALocalCertificateAndRefusesAnUntrustedOne() throws Exception {
    String tmp = System.getenv("TMPDIR");
    Path base = tmp == null ? Path.of(System.getProperty("java.io.tmpdir")) : Path.of(tmp);
    Path dir = Files.createTempDirectory(base, "ts-java-tls");
    try {
      KeyStore keys = KeyStore.getInstance("PKCS12");
      try (var in = Files.newInputStream(keystore(dir))) {
        keys.load(in, "changeit".toCharArray());
      }
      KeyManagerFactory kmf =
          KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
      kmf.init(keys, "changeit".toCharArray());
      TrustManagerFactory tmf =
          TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
      tmf.init(keys);
      SSLContext server = SSLContext.getInstance("TLS");
      server.init(kmf.getKeyManagers(), null, null);
      SSLContext trusting = SSLContext.getInstance("TLS");
      trusting.init(null, tmf.getTrustManagers(), null);

      HttpsServer https =
          HttpsServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
      https.setHttpsConfigurator(new HttpsConfigurator(server));
      try (FakeServer fake = new FakeServer(https)) {
        assertTrue(fake.url().startsWith("https://"));
        try (Client client = Client.create(options(fake).sslContext(trusting))) {
          assertTrue(client.ready());
          assertTrue(client.boolVariation("f", Context.user("u"), false));
          assertEquals("\"v1\"", client.status().etag());
        }
        try (Client client = Client.create(options(fake))) {
          assertFalse(client.ready(), "the default trust store refuses the certificate");
          assertEquals("network", client.status().lastError());
        }
      }
    } finally {
      try (var walk = Files.walk(dir)) {
        walk.sorted(java.util.Comparator.reverseOrder()).forEach(p -> p.toFile().delete());
      }
    }
  }

  @Test
  void anUnclosedClientDoesNotKeepTheJvmAlive() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.stream =
          ex -> {
            OutputStream out = FakeServer.openStream(ex);
            FakeServer.write(out, "event: connected\ndata: \"v1\"\n\n");
            server.released.await();
          };
      List<String> command = new ArrayList<>();
      command.add(Path.of(System.getProperty("java.home"), "bin", "java").toString());
      command.add("-cp");
      command.add(classpath());
      command.add(UnclosedClientMain.class.getName());
      command.add(server.url());
      Process process = new ProcessBuilder(command).redirectErrorStream(true).start();
      boolean exited = process.waitFor(20, TimeUnit.SECONDS);
      String output = new String(exited ? process.getInputStream().readAllBytes() : new byte[0]);
      if (!exited) {
        process.destroyForcibly();
      }
      assertTrue(exited, "the JVM exited by itself");
      assertEquals(0, process.exitValue(), output);
      assertTrue(output.contains("served true"), output);
    }
  }

  private static String classpath() throws Exception {
    Set<String> parts = new TreeSet<>();
    for (Class<?> type : List.of(Client.class, UnclosedClientMain.class)) {
      parts.add(
          Path.of(type.getProtectionDomain().getCodeSource().getLocation().toURI()).toString());
    }
    return String.join(java.io.File.pathSeparator, parts);
  }
}
