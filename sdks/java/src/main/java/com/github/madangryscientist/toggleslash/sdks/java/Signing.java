package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.eval.ContextMessage;
import java.security.AlgorithmParameters;
import java.security.GeneralSecurityException;
import java.security.KeyFactory;
import java.security.PrivateKey;
import java.security.Signature;
import java.security.interfaces.ECPrivateKey;
import java.security.spec.ECGenParameterSpec;
import java.security.spec.ECParameterSpec;
import java.security.spec.PKCS8EncodedKeySpec;
import java.util.Base64;

/**
 * Secure-mode signing (contract section 14): an application server signs each user's context with
 * the environment's signing key and hands the signature to the page for the browser SDK.
 */
public final class Signing {
  private static final ECParameterSpec P256 = p256();

  private Signing() {}

  private static ECParameterSpec p256() {
    try {
      AlgorithmParameters parameters = AlgorithmParameters.getInstance("EC");
      parameters.init(new ECGenParameterSpec("secp256r1"));
      return parameters.getParameterSpec(ECParameterSpec.class);
    } catch (GeneralSecurityException e) {
      throw new IllegalStateException("the JDK has no P-256", e);
    }
  }

  /**
   * Signs a context for secure mode: ECDSA P-256 with SHA-256 over the context's kinds and keys, as
   * the 64-byte IEEE P1363 {@code r || s} in base64url with no padding (86 characters). It needs no
   * {@link Client}. Unlike the rest of the SDK it throws: a bad key or context is a mistake at the
   * call site, not a condition at run time.
   *
   * @param privateKeyPem the PKCS#8 PEM that {@code POST .../signing-key} answered once
   * @param context the context, as the evaluation methods take it
   * @return the signature
   * @throws IllegalArgumentException when the context is not valid, or the key is not an ECDSA
   *     P-256 private key in a PKCS#8 PEM
   */
  public static String signContext(String privateKeyPem, Object context) {
    byte[] message = ContextMessage.of(context);
    if (message == null) {
      throw new IllegalArgumentException(
          "signContext: the context is not valid; see docs/evaluation.md");
    }
    PrivateKey key = readKey(privateKeyPem);
    try {
      Signature signer = Signature.getInstance("SHA256withECDSAinP1363Format");
      signer.initSign(key);
      signer.update(message);
      return Base64.getUrlEncoder().withoutPadding().encodeToString(signer.sign());
    } catch (GeneralSecurityException e) {
      throw new IllegalArgumentException("signContext: the key cannot sign", e);
    }
  }

  private static PrivateKey readKey(String pem) {
    String notP256 = "signContext: the key is not an ECDSA P-256 private key in a PKCS#8 PEM";
    if (pem == null) {
      throw new IllegalArgumentException(notP256);
    }
    String begin = "-----BEGIN PRIVATE KEY-----";
    String end = "-----END PRIVATE KEY-----";
    int from = pem.indexOf(begin);
    int to = pem.indexOf(end);
    if (from < 0 || to < from) {
      throw new IllegalArgumentException(notP256);
    }
    StringBuilder body = new StringBuilder();
    pem.substring(from + begin.length(), to)
        .chars()
        .filter(c -> !Character.isWhitespace(c))
        .forEach(body::appendCodePoint);
    PrivateKey key;
    try {
      byte[] der = Base64.getDecoder().decode(body.toString());
      key = KeyFactory.getInstance("EC").generatePrivate(new PKCS8EncodedKeySpec(der));
    } catch (IllegalArgumentException | GeneralSecurityException e) {
      throw new IllegalArgumentException(notP256, e);
    }
    // KeyFactory reads a P-384 or P-521 key as readily, so the curve is checked here.
    if (!(key instanceof ECPrivateKey ec) || !isP256(ec.getParams())) {
      throw new IllegalArgumentException(notP256);
    }
    return key;
  }

  private static boolean isP256(ECParameterSpec spec) {
    return spec.getCurve().equals(P256.getCurve())
        && spec.getGenerator().equals(P256.getGenerator())
        && spec.getOrder().equals(P256.getOrder())
        && spec.getCofactor() == P256.getCofactor();
  }
}
