package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.eval.ContextMessage;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Utf8;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.Signature;
import java.security.spec.ECGenParameterSpec;
import java.util.Base64;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

/** signContext (contract 14), on every message in fixtures/signing and a key made per test. */
class SigningTest {
  private static final Path MESSAGES = Path.of("../../fixtures/signing/messages.json");

  private static KeyPair keyPair(String curve) throws Exception {
    KeyPairGenerator generator = KeyPairGenerator.getInstance("EC");
    generator.initialize(new ECGenParameterSpec(curve));
    return generator.generateKeyPair();
  }

  private static String pem(KeyPair pair) {
    String body =
        Base64.getMimeEncoder(64, "\n".getBytes(StandardCharsets.US_ASCII))
            .encodeToString(pair.getPrivate().getEncoded());
    return "-----BEGIN PRIVATE KEY-----\n" + body + "\n-----END PRIVATE KEY-----\n";
  }

  private static boolean verifies(KeyPair pair, byte[] message, String signature) throws Exception {
    Signature verifier = Signature.getInstance("SHA256withECDSAinP1363Format");
    verifier.initVerify(pair.getPublic());
    verifier.update(message);
    return verifier.verify(Base64.getUrlDecoder().decode(signature));
  }

  @Test
  @SuppressWarnings("unchecked")
  void signsEveryFixtureMessageSoItVerifies() throws Exception {
    Map<String, Object> file =
        (Map<String, Object>) Json.parse(Utf8.decode(Files.readAllBytes(MESSAGES)));
    List<Object> messages = (List<Object>) file.get("messages");
    assertEquals(7, messages.size());
    KeyPair pair = keyPair("secp256r1");
    for (Object raw : messages) {
      Map<String, Object> entry = (Map<String, Object>) raw;
      String name = (String) entry.get("name");
      byte[] expected = ((String) entry.get("message")).getBytes(StandardCharsets.UTF_8);
      assertArrayEquals(expected, ContextMessage.of(entry.get("context")), name);
      String signature = Signing.signContext(pem(pair), entry.get("context"));
      assertTrue(signature.matches("[A-Za-z0-9_-]{86}"), name + ": " + signature);
      assertTrue(verifies(pair, expected, signature), name);
    }
  }

  @Test
  void aRoundTripVerifiesAndAnotherContextDoesNot() throws Exception {
    KeyPair pair = keyPair("secp256r1");
    Object context = Context.multi(Context.of("user-1"), Context.of("org", "acme"));
    String signature = Signing.signContext(pem(pair), context);
    assertTrue(verifies(pair, ContextMessage.of(context), signature));
    assertTrue(!verifies(pair, ContextMessage.of(Context.of("user-2")), signature));
  }

  @Test
  void refusesAKeyOnAnotherCurve() throws Exception {
    String p384 = pem(keyPair("secp384r1"));
    assertThrows(IllegalArgumentException.class, () -> Signing.signContext(p384, Context.of("u")));
  }

  @Test
  void refusesWhatIsNotAPkcs8Pem() {
    assertThrows(
        IllegalArgumentException.class, () -> Signing.signContext("nope", Context.of("u")));
    assertThrows(IllegalArgumentException.class, () -> Signing.signContext(null, Context.of("u")));
  }

  @Test
  void refusesAnInvalidContext() throws Exception {
    String key = pem(keyPair("secp256r1"));
    assertThrows(IllegalArgumentException.class, () -> Signing.signContext(key, Map.of("key", "")));
    assertThrows(IllegalArgumentException.class, () -> Signing.signContext(key, null));
  }
}
