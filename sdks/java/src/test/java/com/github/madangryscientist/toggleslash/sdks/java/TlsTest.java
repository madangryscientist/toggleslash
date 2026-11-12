package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.InputStream;
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
import org.junit.jupiter.api.io.TempDir;

/**
 * HTTPS against a loopback server with a certificate the test makes, trusted only through the SSL
 * seam.
 */
@Timeout(60)
final class TlsTest {
  private static final char[] PASSWORD = "changeit".toCharArray();

  private static KeyStore selfSigned(Path directory) throws Exception {
    Path store = directory.resolve("tls.p12");
    String keytool = Path.of(System.getProperty("java.home"), "bin", "keytool").toString();
    Process process =
        new ProcessBuilder(
                keytool,
                "-genkeypair",
                "-alias",
                "loopback",
                "-keyalg",
                "RSA",
                "-keysize",
                "2048",
                "-validity",
                "2",
                "-dname",
                "CN=127.0.0.1",
                "-ext",
                "san=ip:127.0.0.1",
                "-storetype",
                "PKCS12",
                "-keystore",
                store.toString(),
                "-storepass",
                new String(PASSWORD))
            .redirectErrorStream(true)
            .start();
    process.getInputStream().readAllBytes();
    assertTrue(process.waitFor(30, TimeUnit.SECONDS));
    assertEquals(0, process.exitValue());
    KeyStore keys = KeyStore.getInstance("PKCS12");
    try (InputStream in = Files.newInputStream(store)) {
      keys.load(in, PASSWORD);
    }
    return keys;
  }

  private static SSLContext serverContext(KeyStore keys) throws Exception {
    KeyManagerFactory factory =
        KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
    factory.init(keys, PASSWORD);
    SSLContext context = SSLContext.getInstance("TLS");
    context.init(factory.getKeyManagers(), null, null);
    return context;
  }

  private static SSLContext clientContext(KeyStore trusted) throws Exception {
    TrustManagerFactory factory =
        TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
    factory.init(trusted);
    SSLContext context = SSLContext.getInstance("TLS");
    context.init(null, factory.getTrustManagers(), null);
    return context;
  }

  private static Options.Builder options(FakeServer server) {
    return Options.builder()
        .url(server.url())
        .sdkKey("fs_server_test")
        .requestTimeout(Duration.ofSeconds(5))
        .flushInterval(Duration.ofHours(1));
  }

  @Test
  void loadsFollowsAndFlushesOverHttpsWhenTheCertificateIsTrusted(@TempDir Path directory)
      throws Exception {
    KeyStore keys = selfSigned(directory);
    try (FakeServer server = new FakeServer(serverContext(keys))) {
      assertTrue(server.url().startsWith("https://"));
      Client client = Client.create(options(server).sslContext(clientContext(keys)).build());
      try {
        assertTrue(client.ready());
        assertTrue(client.boolVariation("checkout", Context.of("u1"), false));
        assertTrue(Eventually.holds(5000, () -> client.status().stream() == StreamState.CONNECTED));
        client.flush();
        assertEquals(1, server.to("/sdk/v1/events").size());
      } finally {
        client.close();
      }
    }
  }

  @Test
  void anUntrustedCertificateIsANetworkErrorAndNeverAFallbackToPlainTrust(@TempDir Path directory)
      throws Exception {
    KeyStore keys = selfSigned(directory);
    try (FakeServer server = new FakeServer(serverContext(keys));
        Client client = Client.create(options(server).build())) {
      assertFalse(client.ready());
      assertEquals(Status.NETWORK, client.status().lastError());
      assertEquals(0, server.requests.size());
    }
  }
}
