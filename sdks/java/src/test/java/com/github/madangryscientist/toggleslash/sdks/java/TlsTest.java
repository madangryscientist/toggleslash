package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.internal.Tuning;
import com.github.madangryscientist.toggleslash.sdks.java.testkit.Fake;
import com.sun.net.httpserver.HttpsConfigurator;
import com.sun.net.httpserver.HttpsServer;
import java.io.InputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.http.HttpClient;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyStore;
import java.time.Duration;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManagerFactory;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** The SDK over TLS against a certificate made for the test. */
@Timeout(60)
class TlsTest {
  private static final char[] PASSWORD = "changeit".toCharArray();

  private static KeyStore keystore() throws Exception {
    String tmp = System.getenv("TMPDIR");
    Path dir =
        Files.createTempDirectory(
            tmp == null ? Path.of(System.getProperty("java.io.tmpdir")) : Path.of(tmp), "tls-");
    Path file = dir.resolve("store.p12");
    String keytool = Path.of(System.getProperty("java.home"), "bin", "keytool").toString();
    Process p =
        new ProcessBuilder(
                keytool,
                "-genkeypair",
                "-alias",
                "fake",
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
                "-storetype",
                "PKCS12",
                "-keystore",
                file.toString(),
                "-storepass",
                new String(PASSWORD))
            .redirectErrorStream(true)
            .start();
    try (InputStream in = p.getInputStream()) {
      in.readAllBytes();
    }
    assertEquals(0, p.waitFor());
    KeyStore store = KeyStore.getInstance("PKCS12");
    try (InputStream in = Files.newInputStream(file)) {
      store.load(in, PASSWORD);
    }
    Files.deleteIfExists(file);
    Files.deleteIfExists(dir);
    return store;
  }

  private static SSLContext context(KeyStore store, boolean server) throws Exception {
    SSLContext ssl = SSLContext.getInstance("TLS");
    if (server) {
      KeyManagerFactory keys =
          KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
      keys.init(store, PASSWORD);
      ssl.init(keys.getKeyManagers(), null, null);
    } else {
      TrustManagerFactory trust =
          TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
      trust.init(store);
      ssl.init(null, trust.getTrustManagers(), null);
    }
    return ssl;
  }

  @Test
  void loadsOverTlsWhenTheCertificateIsTrustedAndRefusesItWhenNot() throws Exception {
    KeyStore store = keystore();
    HttpsServer server =
        HttpsServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
    server.setHttpsConfigurator(new HttpsConfigurator(context(store, true)));
    try (Fake fake = new Fake(server)) {
      fake.ruleset =
          x -> Fake.rulesetReply(x, "\"v1\"", Fake.body(Fake.flag("on", 1, "[false,true]", 1)));
      assertTrue(fake.url().startsWith("https://"));
      Options options =
          Options.builder().url(fake.url()).sdkKey("k").stream(false)
              .requestTimeout(Duration.ofSeconds(5))
              .build();

      Tuning trusting = new Tuning();
      trusting.http =
          HttpClient.newBuilder()
              .version(HttpClient.Version.HTTP_1_1)
              .sslContext(context(store, false))
              .build();
      try (Client trusted = Client.create(options, trusting)) {
        assertTrue(trusted.ready());
        assertTrue(trusted.boolVariation("on", Context.of("u"), false));
        assertEquals("\"v1\"", trusted.status().etag());
      }

      try (Client strict = Client.create(options)) {
        assertFalse(strict.ready());
        assertEquals("network", strict.status().lastError());
      }
    }
  }
}
