package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * UTF-8 to the WHATWG Encoding standard, which the contract names. The JDK's codec differs: a
 * {@code CharsetDecoder} with {@code REPLACE} and {@code String.getBytes} do not give one U+FFFD
 * per maximal invalid subpart, and turn a lone surrogate into {@code ?}.
 */
public final class Utf8 {
  private static final char REPLACEMENT = '�';

  private Utf8() {}

  /** Decodes a whole body: invalid bytes become U+FFFD, a leading byte order mark is dropped. */
  public static String decode(byte[] bytes) {
    Decoder decoder = new Decoder();
    StringBuilder out = new StringBuilder(bytes.length);
    decoder.decode(bytes, 0, bytes.length, out);
    decoder.finish(out);
    return out.toString();
  }

  /**
   * Encodes a string. A lone surrogate has no UTF-8 form, so it is written as U+FFFD, as
   * "Bucketing" in the evaluation spec says for the bytes that are hashed.
   */
  public static byte[] encode(String text) {
    byte[] out = new byte[text.length() * 3];
    int n = 0;
    for (int i = 0; i < text.length(); i++) {
      int c = text.charAt(i);
      if (c < 0x80) {
        out[n++] = (byte) c;
      } else if (c < 0x800) {
        out[n++] = (byte) (0xC0 | (c >> 6));
        out[n++] = (byte) (0x80 | (c & 0x3F));
      } else if (Character.isHighSurrogate((char) c)
          && i + 1 < text.length()
          && Character.isLowSurrogate(text.charAt(i + 1))) {
        int cp = Character.toCodePoint((char) c, text.charAt(i + 1));
        i++;
        out[n++] = (byte) (0xF0 | (cp >> 18));
        out[n++] = (byte) (0x80 | ((cp >> 12) & 0x3F));
        out[n++] = (byte) (0x80 | ((cp >> 6) & 0x3F));
        out[n++] = (byte) (0x80 | (cp & 0x3F));
      } else {
        if (c >= 0xD800 && c <= 0xDFFF) {
          c = REPLACEMENT;
        }
        out[n++] = (byte) (0xE0 | (c >> 12));
        out[n++] = (byte) (0x80 | ((c >> 6) & 0x3F));
        out[n++] = (byte) (0x80 | (c & 0x3F));
      }
    }
    return java.util.Arrays.copyOf(out, n);
  }

  /** A streaming decoder: a sequence split across two reads is held until it completes. */
  public static final class Decoder {
    private int codePoint;
    private int needed;
    private int seen;
    private int lower = 0x80;
    private int upper = 0xBF;
    private boolean atStart = true;

    /** Decodes {@code length} bytes from {@code offset}, appending to {@code out}. */
    public void decode(byte[] bytes, int offset, int length, StringBuilder out) {
      int start = out.length();
      for (int i = offset; i < offset + length; i++) {
        int b = bytes[i] & 0xFF;
        boolean again;
        do {
          again = false;
          if (needed == 0) {
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
              out.append(REPLACEMENT);
            }
          } else if (b < lower || b > upper) {
            reset();
            out.append(REPLACEMENT);
            again = true;
          } else {
            lower = 0x80;
            upper = 0xBF;
            codePoint = (codePoint << 6) | (b & 0x3F);
            seen++;
            if (seen == needed) {
              out.appendCodePoint(codePoint);
              reset();
            }
          }
        } while (again);
      }
      dropBom(out, start);
    }

    /** Ends the input: a sequence cut short is one U+FFFD. */
    public void finish(StringBuilder out) {
      if (needed != 0) {
        reset();
        out.append(REPLACEMENT);
      }
    }

    private void reset() {
      codePoint = 0;
      needed = 0;
      seen = 0;
      lower = 0x80;
      upper = 0xBF;
    }

    private void dropBom(StringBuilder out, int start) {
      if (atStart && out.length() > start) {
        atStart = false;
        if (out.charAt(start) == '﻿') {
          out.deleteCharAt(start);
        }
      }
    }
  }
}
