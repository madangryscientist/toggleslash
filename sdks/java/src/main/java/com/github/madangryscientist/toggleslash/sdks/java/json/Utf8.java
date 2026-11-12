package com.github.madangryscientist.toggleslash.sdks.java.json;

/**
 * The WHATWG Encoding standard's UTF-8 decoder and encoder.
 *
 * <p>The decoder writes one U+FFFD per maximal invalid subpart of the input, not one per byte. The
 * encoder writes a lone surrogate as U+FFFD, not as {@code ?}. The JDK's codecs do neither.
 */
public final class Utf8 {
  private static final char REPLACEMENT = '�';

  private Utf8() {}

  /**
   * Decodes a whole byte array. A byte order mark is not removed.
   *
   * @param bytes the bytes
   * @return the text
   */
  public static String decode(byte[] bytes) {
    Decoder decoder = new Decoder();
    StringBuilder out = new StringBuilder(bytes.length);
    decoder.decode(bytes, 0, bytes.length, out);
    decoder.finish(out);
    return out.toString();
  }

  /**
   * Encodes text as UTF-8, a lone surrogate as U+FFFD.
   *
   * @param text the text
   * @return the bytes
   */
  public static byte[] encode(String text) {
    byte[] out = new byte[text.length() * 3];
    int n = 0;
    for (int i = 0; i < text.length(); i++) {
      int c = text.charAt(i);
      if (c < 0x80) {
        out[n++] = (byte) c;
        continue;
      }
      if (c >= 0xD800 && c <= 0xDFFF) {
        int low = i + 1 < text.length() ? text.charAt(i + 1) : 0;
        if (c <= 0xDBFF && low >= 0xDC00 && low <= 0xDFFF) {
          int cp = 0x10000 + ((c - 0xD800) << 10) + (low - 0xDC00);
          i++;
          out[n++] = (byte) (0xF0 | (cp >> 18));
          out[n++] = (byte) (0x80 | ((cp >> 12) & 0x3F));
          out[n++] = (byte) (0x80 | ((cp >> 6) & 0x3F));
          out[n++] = (byte) (0x80 | (cp & 0x3F));
          continue;
        }
        c = REPLACEMENT;
      }
      if (c < 0x800) {
        out[n++] = (byte) (0xC0 | (c >> 6));
        out[n++] = (byte) (0x80 | (c & 0x3F));
      } else {
        out[n++] = (byte) (0xE0 | (c >> 12));
        out[n++] = (byte) (0x80 | ((c >> 6) & 0x3F));
        out[n++] = (byte) (0x80 | (c & 0x3F));
      }
    }
    return java.util.Arrays.copyOf(out, n);
  }

  /** A decoder that keeps its state across calls, so a character split between reads is whole. */
  public static final class Decoder {
    private int codePoint;
    private int needed;
    private int seen;
    private int lower = 0x80;
    private int upper = 0xBF;

    /** Creates a decoder at the start of a stream. */
    public Decoder() {
      // The fields start at the standard's initial state.
    }

    private void reset() {
      codePoint = 0;
      needed = 0;
      seen = 0;
      lower = 0x80;
      upper = 0xBF;
    }

    /**
     * Decodes more bytes.
     *
     * @param buf the bytes
     * @param off where to start
     * @param len how many
     * @param out where the text goes
     */
    public void decode(byte[] buf, int off, int len, StringBuilder out) {
      int i = off;
      int end = off + len;
      while (i < end) {
        int b = buf[i] & 0xFF;
        if (needed == 0) {
          i++;
          if (b <= 0x7F) {
            out.append((char) b);
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
            out.append(REPLACEMENT);
          }
          continue;
        }
        if (b < lower || b > upper) {
          // The sequence ends here; this byte is read again from the start state.
          reset();
          out.append(REPLACEMENT);
          continue;
        }
        i++;
        lower = 0x80;
        upper = 0xBF;
        codePoint = (codePoint << 6) | (b & 0x3F);
        seen++;
        if (seen == needed) {
          out.appendCodePoint(codePoint);
          reset();
        }
      }
    }

    /**
     * Ends the stream: an unfinished sequence is one U+FFFD.
     *
     * @param out where the text goes
     */
    public void finish(StringBuilder out) {
      if (needed != 0) {
        out.append(REPLACEMENT);
        reset();
      }
    }
  }
}
