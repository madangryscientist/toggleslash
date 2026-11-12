package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * UTF-8 as the WHATWG Encoding standard has it. The JDK's codecs are not used: a {@code
 * CharsetDecoder} with {@code REPLACE} and {@code String.getBytes} differ from the standard on how
 * many U+FFFD an invalid sequence becomes and on a lone surrogate.
 */
public final class Utf8 {
  private static final int REPLACEMENT = 0xFFFD;

  private Utf8() {}

  /**
   * Decodes bytes, dropping one leading byte order mark and writing one U+FFFD per maximal invalid
   * subpart.
   *
   * @param bytes the bytes
   * @param offset where to start
   * @param length how many bytes
   * @return the text
   */
  public static String decode(byte[] bytes, int offset, int length) {
    StringBuilder out = new StringBuilder(length);
    Decoder decoder = new Decoder(true);
    decoder.feed(bytes, offset, length, out);
    decoder.finish(out);
    return out.toString();
  }

  /**
   * Decodes a whole array.
   *
   * @param bytes the bytes
   * @return the text
   */
  public static String decode(byte[] bytes) {
    return decode(bytes, 0, bytes.length);
  }

  /**
   * Encodes text. A lone surrogate becomes U+FFFD.
   *
   * @param text the text
   * @return the bytes
   */
  public static byte[] encode(String text) {
    byte[] out = new byte[text.length() * 3];
    int n = 0;
    for (int i = 0; i < text.length(); i++) {
      int c = text.charAt(i);
      if (Character.isHighSurrogate((char) c)
          && i + 1 < text.length()
          && Character.isLowSurrogate(text.charAt(i + 1))) {
        c = Character.toCodePoint((char) c, text.charAt(i + 1));
        i++;
      } else if (c >= 0xD800 && c <= 0xDFFF) {
        c = REPLACEMENT;
      }
      if (c < 0x80) {
        out[n++] = (byte) c;
      } else if (c < 0x800) {
        out[n++] = (byte) (0xC0 | (c >> 6));
        out[n++] = (byte) (0x80 | (c & 0x3F));
      } else if (c < 0x10000) {
        out[n++] = (byte) (0xE0 | (c >> 12));
        out[n++] = (byte) (0x80 | ((c >> 6) & 0x3F));
        out[n++] = (byte) (0x80 | (c & 0x3F));
      } else {
        out[n++] = (byte) (0xF0 | (c >> 18));
        out[n++] = (byte) (0x80 | ((c >> 12) & 0x3F));
        out[n++] = (byte) (0x80 | ((c >> 6) & 0x3F));
        out[n++] = (byte) (0x80 | (c & 0x3F));
      }
    }
    return java.util.Arrays.copyOf(out, n);
  }

  /** An incremental decoder: a character split across two reads is kept for the next one. */
  public static final class Decoder {
    private int needed;
    private int seen;
    private int lower = 0x80;
    private int upper = 0xBF;
    private int codePoint;
    private boolean stripBom;

    /**
     * Creates a decoder.
     *
     * @param stripBom whether to drop a byte order mark at the very start
     */
    public Decoder(boolean stripBom) {
      this.stripBom = stripBom;
    }

    /**
     * Decodes more bytes.
     *
     * @param bytes the bytes
     * @param offset where to start
     * @param length how many bytes
     * @param out where the text goes
     */
    public void feed(byte[] bytes, int offset, int length, StringBuilder out) {
      int end = offset + length;
      for (int i = offset; i < end; i++) {
        int b = bytes[i] & 0xFF;
        if (needed == 0) {
          start(b, out);
        } else if (b < lower || b > upper) {
          reset();
          emit(REPLACEMENT, out);
          i--;
        } else {
          lower = 0x80;
          upper = 0xBF;
          codePoint = (codePoint << 6) | (b & 0x3F);
          seen++;
          if (seen == needed) {
            int done = codePoint;
            reset();
            emit(done, out);
          }
        }
      }
    }

    /**
     * Ends the input: an unfinished character becomes one U+FFFD.
     *
     * @param out where the text goes
     */
    public void finish(StringBuilder out) {
      if (needed != 0) {
        reset();
        emit(REPLACEMENT, out);
      }
    }

    private void start(int b, StringBuilder out) {
      if (b <= 0x7F) {
        emit(b, out);
      } else if (b >= 0xC2 && b <= 0xDF) {
        needed = 1;
        codePoint = b & 0x1F;
      } else if (b >= 0xE0 && b <= 0xEF) {
        if (b == 0xE0) {
          lower = 0xA0;
        }
        if (b == 0xED) {
          upper = 0x9F;
        }
        needed = 2;
        codePoint = b & 0xF;
      } else if (b >= 0xF0 && b <= 0xF4) {
        if (b == 0xF0) {
          lower = 0x90;
        }
        if (b == 0xF4) {
          upper = 0x8F;
        }
        needed = 3;
        codePoint = b & 0x7;
      } else {
        emit(REPLACEMENT, out);
      }
    }

    private void reset() {
      needed = 0;
      seen = 0;
      codePoint = 0;
      lower = 0x80;
      upper = 0xBF;
    }

    private void emit(int cp, StringBuilder out) {
      if (stripBom) {
        stripBom = false;
        if (cp == 0xFEFF) {
          return;
        }
      }
      out.appendCodePoint(cp);
    }
  }
}
