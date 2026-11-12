package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.InputStream;
import java.net.InetAddress;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyStore;
import java.time.Duration;
import java.util.concurrent.TimeUnit;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLServerSocket;
import javax.net.ssl.TrustManagerFactory;
import org.junit.jupiter.api.Test;

/** An {@code https} URL reaches the TLS path, against a certificate made for the test. */
class TlsTest {
  private static final char[] PASSWORD = "changeit".toCharArray();

  private static KeyStore keystore() throws Exception {
    Path dir = Files.createDirectories(Path.of("target", "tls"));
    Path file = dir.resolve("keystore.p12");
    Files.deleteIfExists(file);
    String keytool = Path.of(System.getProperty("java.home"), "bin", "keytool").toString();
    Process process =
        new ProcessBuilder(
                keytool,
                "-genkeypair",
                "-alias",
                "test",
                "-keyalg",
                "RSA",
                "-keysize",
                "2048",
                "-validity",
                "2",
                "-dname",
                "CN=localhost",
                "-ext",
                "san=ip:127.0.0.1",
                "-keystore",
                file.toString(),
                "-storetype",
                "PKCS12",
                "-storepass",
                "changeit",
                "-keypass",
                "changeit")
            .redirectErrorStream(true)
            .start();
    process.getInputStream().readAllBytes();
    assertTrue(process.waitFor(60, TimeUnit.SECONDS));
    assertEquals(0, process.exitValue());
    KeyStore store = KeyStore.getInstance("PKCS12");
    try (InputStream in = Files.newInputStream(file)) {
      store.load(in, PASSWORD);
    }
    return store;
  }

  private static FakeServer tlsServer(KeyStore store) throws Exception {
    KeyManagerFactory keys = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
    keys.init(store, PASSWORD);
    SSLContext context = SSLContext.getInstance("TLS");
    context.init(keys.getKeyManagers(), null, null);
    SSLServerSocket socket =
        (SSLServerSocket)
            context
                .getServerSocketFactory()
                .createServerSocket(0, 50, InetAddress.getLoopbackAddress());
    FakeServer server = new FakeServer(socket, "https");
    server.handler((request, conn) -> conn.respond(200, Support.boolFlags(), "ETag", "\"v1\""));
    return server;
  }

  @Test
  void aTrustedCertificateLoadsTheRulesetOverTls() throws Exception {
    KeyStore store = keystore();
    TrustManagerFactory trust =
        TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
    trust.init(store);
    SSLContext client = SSLContext.getInstance("TLS");
    client.init(null, trust.getTrustManagers(), null);
    try (FakeServer server = tlsServer(store);
        Client sdk =
            Client.create(Support.options(server).stream(false).sslContext(client).build())) {
      assertTrue(server.url().startsWith("https://"));
      assertTrue(sdk.ready());
      assertTrue(sdk.boolVariation("f", java.util.Map.of("kind", "user", "key", "u"), false));
    }
  }

  @Test
  void anUntrustedCertificateIsRefusedAsANetworkError() throws Exception {
    KeyStore store = keystore();
    try (FakeServer server = tlsServer(store);
        Client sdk =
            Client.create(
                Support.options(server).stream(false)
                    .requestTimeout(Duration.ofSeconds(5))
                    .build())) {
      assertFalse(sdk.ready());
      assertEquals("network", sdk.status().lastError());
    }
  }
}
