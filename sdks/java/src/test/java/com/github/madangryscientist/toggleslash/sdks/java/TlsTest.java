package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.File;
import java.io.InputStream;
import java.net.http.HttpClient;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyStore;
import java.time.Duration;
import java.util.concurrent.TimeUnit;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManagerFactory;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/**
 * An {@code https} URL reaches the TLS path: a server with a certificate made here, which only a
 * client that trusts it can talk to. The SDK adds no TLS code of its own: it asks the JDK's {@code
 * HttpClient}, so the trust comes from the client a test hands in, and the default client refuses
 * the certificate.
 */
@Timeout(120)
final class TlsTest {
  private static SSLContext context(Path dir) throws Exception {
    Path store = dir.resolve("ks.p12");
    String keytool =
        System.getProperty("java.home") + File.separator + "bin" + File.separator + "keytool";
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
                "-validity",
                "2",
                "-dname",
                "CN=localhost",
                "-ext",
                "san=ip:127.0.0.1,dns:localhost",
                "-keystore",
                store.toString(),
                "-storetype",
                "PKCS12",
                "-storepass",
                "changeit",
                "-keypass",
                "changeit")
            .redirectErrorStream(true)
            .start();
    String output = new String(process.getInputStream().readAllBytes());
    assertTrue(process.waitFor(60, TimeUnit.SECONDS), output);
    assertEquals(0, process.exitValue(), output);
    KeyStore keys = KeyStore.getInstance("PKCS12");
    try (InputStream in = Files.newInputStream(store)) {
      keys.load(in, "changeit".toCharArray());
    }
    KeyManagerFactory kmf = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
    kmf.init(keys, "changeit".toCharArray());
    TrustManagerFactory tmf =
        TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
    tmf.init(keys);
    SSLContext ssl = SSLContext.getInstance("TLS");
    ssl.init(kmf.getKeyManagers(), tmf.getTrustManagers(), null);
    return ssl;
  }

  @Test
  void anHttpsUrlLoadsTheRulesetFollowsTheStreamAndSendsUsage() throws Exception {
    Path dir = Files.createTempDirectory(Path.of(System.getProperty("java.io.tmpdir")), "tls");
    try {
      SSLContext ssl = context(dir);
      try (FakeServer server = new FakeServer(ssl)) {
        assertTrue(server.url().startsWith("https://"));
        server.ruleset =
            FakeServer.serving("\"v1\"", Support.ruleset(Support.boolFlag("checkout", true, 1)));
        server.stream = server.sse("event: connected\ndata: \"v1\"\n\n");
        HttpClient trusting =
            HttpClient.newBuilder().sslContext(ssl).version(HttpClient.Version.HTTP_1_1).build();
        Client client =
            Client.create(
                Support.options(server)
                    .httpClient(trusting)
                    .flushInterval(Duration.ofMillis(100))
                    .requestTimeout(Duration.ofSeconds(5))
                    .build());
        assertTrue(client.ready());
        Support.until(
            "connected over TLS", 5000, () -> client.status().stream() == StreamState.CONNECTED);
        assertTrue(
            client.booleanVariation(
                "checkout", java.util.Map.of("kind", "user", "key", "u"), false));
        Support.until("usage sent over TLS", 5000, () -> !server.to("/sdk/v1/events").isEmpty());
        client.close();
      }
      try (FakeServer server = new FakeServer(ssl)) {
        server.ruleset =
            FakeServer.serving("\"v1\"", Support.ruleset(Support.boolFlag("checkout", true, 1)));
        Client untrusting =
            Client.create(Support.options(server).requestTimeout(Duration.ofSeconds(5)).build());
        assertFalse(untrusting.ready(), "the default client does not trust the certificate");
        assertEquals(Status.NETWORK, untrusting.status().lastError());
        assertTrue(server.to("/sdk/v1/ruleset").isEmpty(), "no request got past the handshake");
        untrusting.close();
      }
    } finally {
      try (var files = Files.walk(dir)) {
        files.sorted(java.util.Comparator.reverseOrder()).forEach(p -> p.toFile().delete());
      }
    }
  }
}
