package com.github.madangryscientist.toggleslash.sdks.java.eval;

import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * The bytes a secure-mode signature covers ({@code docs/sdk-browser.md}, "Secure mode"): the
 * context normalised as the evaluator does it, keys only, the kinds in byte order, each kind and
 * then its key as a netstring. Attributes are not part of it.
 */
public final class ContextMessage {
  private ContextMessage() {}

  /**
   * The signed bytes of a context.
   *
   * @param context a map, or a value of any type (which is then not a valid context)
   * @return the bytes, or null when the context is not valid
   */
  public static byte[] of(Object context) {
    Contexts contexts = Contexts.normalise(context);
    if (contexts == null) {
      return null;
    }
    List<Map.Entry<String, Contexts.KindContext>> entries = new ArrayList<>();
    contexts.entries().forEach(entries::add);
    // A kind is ASCII, so String order is byte order, and no two entries share a kind.
    entries.sort(Map.Entry.comparingByKey());
    ByteArrayOutputStream out = new ByteArrayOutputStream();
    for (Map.Entry<String, Contexts.KindContext> entry : entries) {
      netstring(out, entry.getKey());
      netstring(out, entry.getValue().key());
    }
    return out.toByteArray();
  }

  /**
   * {@code <byte length of the UTF-8>:<UTF-8 bytes>,}, a lone surrogate written as U+FFFD as the
   * other SDKs and the server write it ({@link Utf8#encode}; the JDK's encoder writes a question
   * mark).
   */
  private static void netstring(ByteArrayOutputStream out, String text) {
    byte[] bytes = Utf8.encode(text);
    out.writeBytes((bytes.length + ":").getBytes(StandardCharsets.US_ASCII));
    out.writeBytes(bytes);
    out.write(',');
  }
}
