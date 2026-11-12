package com.github.madangryscientist.toggleslash.sdks.java.eval;

/**
 * UTF-8 as the WHATWG Encoding standard defines it, written by hand because the JDK's codec
 * differs: the decoder yields one U+FFFD per maximal invalid subpart, and the encoder turns a lone
 * surrogate into U+FFFD instead of a question mark.
 */
public final class Utf8 {
  private Utf8() {}

  /**
   * An incremental decoder: it keeps a half sequence between calls, so a character split across two
   * reads decodes whole.
   */
  public static final class Decoder {
    private int codePoint;
    private int bytesSeen;
    private int bytesNeeded;
    private int lower = 0x80;
    private int upper = 0xbf;

    /**
     * Decodes bytes and appends the text.
     *
     * @param bytes the buffer
     * @param offset where to start
     * @param length how many bytes
     * @param out where the text goes
     */
    public void decode(byte[] bytes, int offset, int length, StringBuilder out) {
      int end = offset + length;
      for (int i = offset; i < end; ) {
        int b = bytes[i] & 0xff;
        if (bytesNeeded == 0) {
          i++;
          start(b, out);
          continue;
        }
        if (b < lower || b > upper) {
          reset();
          out.append('�');
          // The byte is read again as the start of something new.
          continue;
        }
        i++;
        lower = 0x80;
        upper = 0xbf;
        codePoint = (codePoint << 6) | (b & 0x3f);
        bytesSeen++;
        if (bytesSeen == bytesNeeded) {
          out.appendCodePoint(codePoint);
          reset();
        }
      }
    }

    /**
     * Ends the input: a half sequence left over is one U+FFFD.
     *
     * @param out where the text goes
     */
    public void finish(StringBuilder out) {
      if (bytesNeeded != 0) {
        out.append('�');
      }
      reset();
    }

    private void reset() {
      codePoint = 0;
      bytesSeen = 0;
      bytesNeeded = 0;
      lower = 0x80;
      upper = 0xbf;
    }

    private void start(int b, StringBuilder out) {
      if (b <= 0x7f) {
        out.append((char) b);
      } else if (b >= 0xc2 && b <= 0xdf) {
        bytesNeeded = 1;
        codePoint = b & 0x1f;
      } else if (b >= 0xe0 && b <= 0xef) {
        if (b == 0xe0) {
          lower = 0xa0;
        }
        if (b == 0xed) {
          upper = 0x9f;
        }
        bytesNeeded = 2;
        codePoint = b & 0xf;
      } else if (b >= 0xf0 && b <= 0xf4) {
        if (b == 0xf0) {
          lower = 0x90;
        }
        if (b == 0xf4) {
          upper = 0x8f;
        }
        bytesNeeded = 3;
        codePoint = b & 0x7;
      } else {
        out.append('�');
      }
    }
  }

  /**
   * Decodes a whole byte array; a leading byte order mark is removed.
   *
   * @param bytes the bytes
   * @return the text
   */
  public static String decode(byte[] bytes) {
    StringBuilder out = new StringBuilder(bytes.length);
    Decoder decoder = new Decoder();
    decoder.decode(bytes, 0, bytes.length, out);
    decoder.finish(out);
    if (out.length() > 0 && out.charAt(0) == '﻿') {
      out.deleteCharAt(0);
    }
    return out.toString();
  }

  /**
   * Encodes text, turning a lone surrogate into U+FFFD.
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
      } else if (c >= 0xd800 && c <= 0xdfff) {
        c = 0xfffd;
      }
      if (c < 0x80) {
        out[n++] = (byte) c;
      } else if (c < 0x800) {
        out[n++] = (byte) (0xc0 | (c >> 6));
        out[n++] = (byte) (0x80 | (c & 0x3f));
      } else if (c < 0x10000) {
        out[n++] = (byte) (0xe0 | (c >> 12));
        out[n++] = (byte) (0x80 | ((c >> 6) & 0x3f));
        out[n++] = (byte) (0x80 | (c & 0x3f));
      } else {
        out[n++] = (byte) (0xf0 | (c >> 18));
        out[n++] = (byte) (0x80 | ((c >> 12) & 0x3f));
        out[n++] = (byte) (0x80 | ((c >> 6) & 0x3f));
        out[n++] = (byte) (0x80 | (c & 0x3f));
      }
    }
    return java.util.Arrays.copyOf(out, n);
  }
}
