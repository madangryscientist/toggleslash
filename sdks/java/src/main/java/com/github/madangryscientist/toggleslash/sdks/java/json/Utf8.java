package com.github.madangryscientist.toggleslash.sdks.java.json;

/**
 * UTF-8 as the WHATWG Encoding standard defines it, written by hand.
 *
 * <p>The decoder replaces every maximal bad subpart with one U+FFFD, exactly as the standard's
 * decoder does, and never throws. The encoder turns a lone surrogate into U+FFFD, which {@code
 * String.getBytes} does not (it writes {@code ?}).
 */
public final class Utf8 {

  private static final int REPLACEMENT = 0xFFFD;

  private Utf8() {}

  /**
   * Decodes a whole byte array, without stripping a leading byte order mark.
   *
   * @param bytes the bytes
   * @return the text
   */
  public static String decode(byte[] bytes) {
    return decode(bytes, 0, bytes.length);
  }

  /**
   * Decodes part of a byte array, without stripping a leading byte order mark.
   *
   * @param bytes the bytes
   * @param offset the first byte
   * @param length how many bytes
   * @return the text
   */
  public static String decode(byte[] bytes, int offset, int length) {
    Decoder decoder = new Decoder(false);
    StringBuilder out = new StringBuilder(length);
    decoder.decode(bytes, offset, length, false, out);
    decoder.decode(bytes, 0, 0, true, out);
    return out.toString();
  }

  /**
   * Encodes text as UTF-8, with a lone surrogate as U+FFFD.
   *
   * @param text the text
   * @return the bytes
   */
  public static byte[] encode(String text) {
    byte[] out = new byte[text.length() * 3];
    int n = 0;
    for (int i = 0; i < text.length(); i++) {
      int c = text.charAt(i);
      if (c >= 0xD800 && c <= 0xDBFF && i + 1 < text.length()) {
        int d = text.charAt(i + 1);
        if (d >= 0xDC00 && d <= 0xDFFF) {
          c = 0x10000 + ((c - 0xD800) << 10) + (d - 0xDC00);
          i++;
        }
      }
      if (c >= 0xD800 && c <= 0xDFFF) {
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

  /** A streaming decoder: chunks may split a sequence anywhere. */
  public static final class Decoder {
    private final boolean skipBom;
    private boolean first = true;
    private int codePoint;
    private int needed;
    private int seen;
    private int lower = 0x80;
    private int upper = 0xBF;

    /**
     * Creates a decoder.
     *
     * @param skipBom whether a U+FEFF at the very start is dropped
     */
    public Decoder(boolean skipBom) {
      this.skipBom = skipBom;
    }

    /**
     * Decodes a chunk and appends the text to {@code out}.
     *
     * @param bytes the bytes
     * @param offset the first byte
     * @param length how many bytes
     * @param end true for the last call: an unfinished sequence becomes U+FFFD
     * @param out where the text goes
     */
    public void decode(byte[] bytes, int offset, int length, boolean end, StringBuilder out) {
      int i = offset;
      int stop = offset + length;
      while (i < stop) {
        int b = bytes[i] & 0xFF;
        if (needed == 0) {
          i++;
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
            codePoint = b & 0x0F;
          } else if (b >= 0xF0 && b <= 0xF4) {
            if (b == 0xF0) {
              lower = 0x90;
            }
            if (b == 0xF4) {
              upper = 0x8F;
            }
            needed = 3;
            codePoint = b & 0x07;
          } else {
            emit(REPLACEMENT, out);
          }
        } else if (b < lower || b > upper) {
          reset();
          emit(REPLACEMENT, out);
          // The byte is read again as the start of something new.
        } else {
          i++;
          lower = 0x80;
          upper = 0xBF;
          codePoint = (codePoint << 6) | (b & 0x3F);
          seen++;
          if (seen == needed) {
            int point = codePoint;
            reset();
            emit(point, out);
          }
        }
      }
      if (end && needed != 0) {
        reset();
        emit(REPLACEMENT, out);
      }
    }

    private void reset() {
      codePoint = 0;
      needed = 0;
      seen = 0;
      lower = 0x80;
      upper = 0xBF;
    }

    private void emit(int point, StringBuilder out) {
      if (first) {
        first = false;
        if (skipBom && point == 0xFEFF) {
          return;
        }
      }
      out.appendCodePoint(point);
    }
  }
}
