package com.github.madangryscientist.toggleslash.sdks.java.json;

import java.io.ByteArrayOutputStream;

/**
 * The WHATWG Encoding standard's UTF-8 decoder and encoder.
 *
 * <p>The JDK's codec differs: a {@code CharsetDecoder} may replace a whole run of invalid bytes
 * with one replacement character, and {@code String.getBytes} writes {@code ?} for a lone
 * surrogate. The contract and the spec need one U+FFFD per maximal invalid subpart when decoding
 * and U+FFFD for a lone surrogate when encoding.
 */
public final class Utf8 {
  private static final int REPLACEMENT = 0xFFFD;

  private int needed;
  private int seen;
  private int codePoint;
  private int lower = 0x80;
  private int upper = 0xBF;

  /** Makes a decoder for a text that arrives in pieces. */
  public Utf8() {
    // Starts at the first byte of a sequence.
  }

  /**
   * Decodes a whole byte array. The byte order mark is not removed here.
   *
   * @param bytes the bytes
   * @return the text, with one U+FFFD per maximal invalid subpart
   */
  public static String decode(byte[] bytes) {
    StringBuilder out = new StringBuilder(bytes.length);
    Utf8 decoder = new Utf8();
    decoder.feed(bytes, 0, bytes.length, out);
    decoder.finish(out);
    return out.toString();
  }

  /**
   * Decodes more bytes. An incomplete sequence at the end of the piece is held for the next call.
   *
   * @param bytes the buffer
   * @param offset the first byte to read
   * @param length how many bytes to read
   * @param out where the decoded text goes
   */
  public void feed(byte[] bytes, int offset, int length, StringBuilder out) {
    int end = offset + length;
    for (int i = offset; i < end; i++) {
      step(bytes[i] & 0xFF, out);
    }
  }

  /**
   * Ends the input: a sequence left incomplete becomes one U+FFFD.
   *
   * @param out where the decoded text goes
   */
  public void finish(StringBuilder out) {
    if (needed != 0) {
      reset();
      out.append((char) REPLACEMENT);
    }
  }

  private void reset() {
    needed = 0;
    seen = 0;
    codePoint = 0;
    lower = 0x80;
    upper = 0xBF;
  }

  private void step(int b, StringBuilder out) {
    if (needed == 0) {
      start(b, out);
      return;
    }
    if (b < lower || b > upper) {
      reset();
      out.append((char) REPLACEMENT);
      step(b, out);
      return;
    }
    lower = 0x80;
    upper = 0xBF;
    codePoint = (codePoint << 6) | (b & 0x3F);
    seen++;
    if (seen == needed) {
      int done = codePoint;
      reset();
      out.appendCodePoint(done);
    }
  }

  private void start(int b, StringBuilder out) {
    if (b <= 0x7F) {
      out.append((char) b);
    } else if (b >= 0xC2 && b <= 0xDF) {
      needed = 1;
      codePoint = b & 0x1F;
    } else if (b >= 0xE0 && b <= 0xEF) {
      if (b == 0xE0) {
        lower = 0xA0;
      } else if (b == 0xED) {
        upper = 0x9F;
      }
      needed = 2;
      codePoint = b & 0x0F;
    } else if (b >= 0xF0 && b <= 0xF4) {
      if (b == 0xF0) {
        lower = 0x90;
      } else if (b == 0xF4) {
        upper = 0x8F;
      }
      needed = 3;
      codePoint = b & 0x07;
    } else {
      out.append((char) REPLACEMENT);
    }
  }

  /**
   * Encodes text as UTF-8. A lone surrogate is written as U+FFFD, as the spec's hashing needs.
   *
   * @param text the text
   * @return the bytes
   */
  public static byte[] encode(String text) {
    ByteArrayOutputStream out = new ByteArrayOutputStream(text.length() + 16);
    int i = 0;
    int n = text.length();
    while (i < n) {
      char c = text.charAt(i++);
      int cp = c;
      if (Character.isHighSurrogate(c) && i < n && Character.isLowSurrogate(text.charAt(i))) {
        cp = Character.toCodePoint(c, text.charAt(i++));
      } else if (Character.isSurrogate(c)) {
        cp = REPLACEMENT;
      }
      write(out, cp);
    }
    return out.toByteArray();
  }

  private static void write(ByteArrayOutputStream out, int cp) {
    if (cp < 0x80) {
      out.write(cp);
    } else if (cp < 0x800) {
      out.write(0xC0 | (cp >> 6));
      out.write(0x80 | (cp & 0x3F));
    } else if (cp < 0x10000) {
      out.write(0xE0 | (cp >> 12));
      out.write(0x80 | ((cp >> 6) & 0x3F));
      out.write(0x80 | (cp & 0x3F));
    } else {
      out.write(0xF0 | (cp >> 18));
      out.write(0x80 | ((cp >> 12) & 0x3F));
      out.write(0x80 | ((cp >> 6) & 0x3F));
      out.write(0x80 | (cp & 0x3F));
    }
  }
}
