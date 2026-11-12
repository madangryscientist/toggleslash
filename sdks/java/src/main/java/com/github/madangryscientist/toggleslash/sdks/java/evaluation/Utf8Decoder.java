package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * A UTF-8 decoder that follows the WHATWG Encoding standard: one U+FFFD per maximal invalid
 * subpart. The JDK's decoder does not (it answers one U+FFFD for the three bytes of an encoded
 * surrogate). It keeps its state between calls, so a stream can be fed in chunks. Not thread-safe.
 */
public final class Utf8Decoder {

  private int needed;
  private int seen;
  private int codePoint;
  private int lower = 0x80;
  private int upper = 0xBF;

  /** Decodes a whole array. */
  public static String decode(byte[] bytes, int offset, int length) {
    StringBuilder out = new StringBuilder(length);
    Utf8Decoder decoder = new Utf8Decoder();
    decoder.feed(bytes, offset, length, out);
    decoder.finish(out);
    return out.toString();
  }

  /** Decodes more bytes into {@code out}; a sequence cut at the end waits for the next call. */
  public void feed(byte[] bytes, int offset, int length, StringBuilder out) {
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
          codePoint = b & 0x1F;
        } else if (b >= 0xE0 && b <= 0xEF) {
          if (b == 0xE0) {
            lower = 0xA0;
          } else if (b == 0xED) {
            upper = 0x9F;
          }
          needed = 2;
          codePoint = b & 0xF;
        } else if (b >= 0xF0 && b <= 0xF4) {
          if (b == 0xF0) {
            lower = 0x90;
          } else if (b == 0xF4) {
            upper = 0x8F;
          }
          needed = 3;
          codePoint = b & 0x7;
        } else {
          out.append('�');
        }
        continue;
      }
      if (b < lower || b > upper) {
        reset();
        out.append('�');
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

  /** Ends the input: a sequence still open becomes one U+FFFD. */
  public void finish(StringBuilder out) {
    if (needed != 0) {
      out.append('�');
      reset();
    }
  }

  private void reset() {
    needed = 0;
    seen = 0;
    codePoint = 0;
    lower = 0x80;
    upper = 0xBF;
  }
}
