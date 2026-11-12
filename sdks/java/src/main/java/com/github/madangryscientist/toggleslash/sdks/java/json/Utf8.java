package com.github.madangryscientist.toggleslash.sdks.java.json;

/**
 * A hand-written UTF-8 decoder and encoder that follow the WHATWG Encoding standard. The JDK codec
 * differs on two points the SDK contract names: it does not give one U+FFFD per maximal invalid
 * subpart, and a lone surrogate becomes a question mark instead of U+FFFD.
 */
public final class Utf8 {
  private static final char REPLACEMENT = '�';

  private Utf8() {}

  /**
   * An incremental decoder. A sequence split across two {@code write} calls decodes as if the bytes
   * had arrived together.
   */
  public static final class Decoder {
    private int needed;
    private int seen;
    private int point;
    private int lower = 0x80;
    private int upper = 0xBF;

    /** Creates a decoder with no pending bytes. */
    public Decoder() {}

    private void reset() {
      needed = 0;
      seen = 0;
      point = 0;
      lower = 0x80;
      upper = 0xBF;
    }

    /**
     * Decodes {@code length} bytes from {@code offset} and appends the text to {@code out}.
     *
     * @param bytes the buffer to read from
     * @param offset the first byte to read
     * @param length how many bytes to read
     * @param out where the decoded text goes
     */
    public void write(byte[] bytes, int offset, int length, StringBuilder out) {
      int end = offset + length;
      int i = offset;
      while (i < end) {
        int b = bytes[i] & 0xFF;
        if (needed == 0) {
          i++;
          if (b <= 0x7F) {
            out.append((char) b);
          } else if (b >= 0xC2 && b <= 0xDF) {
            needed = 1;
            point = b & 0x1F;
          } else if (b >= 0xE0 && b <= 0xEF) {
            if (b == 0xE0) {
              lower = 0xA0;
            }
            if (b == 0xED) {
              upper = 0x9F;
            }
            needed = 2;
            point = b & 0xF;
          } else if (b >= 0xF0 && b <= 0xF4) {
            if (b == 0xF0) {
              lower = 0x90;
            }
            if (b == 0xF4) {
              upper = 0x8F;
            }
            needed = 3;
            point = b & 0x7;
          } else {
            out.append(REPLACEMENT);
          }
        } else if (b < lower || b > upper) {
          // The byte is not consumed: it is read again as the start of the next sequence.
          reset();
          out.append(REPLACEMENT);
        } else {
          i++;
          lower = 0x80;
          upper = 0xBF;
          point = (point << 6) | (b & 0x3F);
          seen++;
          if (seen == needed) {
            out.appendCodePoint(point);
            reset();
          }
        }
      }
    }

    /**
     * Ends the input: an unfinished sequence becomes one U+FFFD.
     *
     * @param out where the last character goes
     */
    public void finish(StringBuilder out) {
      if (needed != 0) {
        reset();
        out.append(REPLACEMENT);
      }
    }
  }

  /**
   * Decodes a whole byte range.
   *
   * @param bytes the buffer to read from
   * @param offset the first byte to read
   * @param length how many bytes to read
   * @return the text
   */
  public static String decode(byte[] bytes, int offset, int length) {
    StringBuilder out = new StringBuilder(length);
    Decoder decoder = new Decoder();
    decoder.write(bytes, offset, length, out);
    decoder.finish(out);
    return out.toString();
  }

  /**
   * Encodes text. A lone surrogate becomes U+FFFD, as the Encoding standard says.
   *
   * @param text the text
   * @return the UTF-8 bytes
   */
  public static byte[] encode(String text) {
    byte[] out = new byte[text.length() * 3];
    int n = 0;
    int length = text.length();
    for (int i = 0; i < length; i++) {
      int c = text.charAt(i);
      if (c >= 0xD800 && c <= 0xDBFF && i + 1 < length) {
        int low = text.charAt(i + 1);
        if (low >= 0xDC00 && low <= 0xDFFF) {
          c = ((c - 0xD800) << 10) + (low - 0xDC00) + 0x10000;
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
}
