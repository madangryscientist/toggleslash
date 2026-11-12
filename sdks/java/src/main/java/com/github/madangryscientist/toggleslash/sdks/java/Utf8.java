package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.ByteArrayOutputStream;

/**
 * UTF-8 as the WHATWG Encoding standard reads and writes it. The JDK's codec differs: a decoder set
 * to replace writes one U+FFFD per bad byte in places the standard writes one per maximal invalid
 * subpart, and {@code String.getBytes} writes {@code ?} for a lone surrogate where the standard
 * writes U+FFFD.
 */
final class Utf8 {
  private Utf8() {}

  /** Decodes {@code bytes} with the standard's UTF-8 decoder, never failing. A BOM is kept. */
  static String decode(byte[] bytes) {
    return decode(bytes, 0, bytes.length);
  }

  static String decode(byte[] bytes, int offset, int length) {
    StringBuilder out = new StringBuilder(length);
    int needed = 0;
    int seen = 0;
    int codePoint = 0;
    int lower = 0x80;
    int upper = 0xBF;
    int end = offset + length;
    for (int i = offset; i < end; i++) {
      int b = bytes[i] & 0xFF;
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
          out.append('�');
        }
        continue;
      }
      if (b < lower || b > upper) {
        codePoint = 0;
        needed = 0;
        seen = 0;
        lower = 0x80;
        upper = 0xBF;
        i--;
        out.append('�');
        continue;
      }
      lower = 0x80;
      upper = 0xBF;
      codePoint = (codePoint << 6) | (b & 0x3F);
      seen++;
      if (seen == needed) {
        out.appendCodePoint(codePoint);
        codePoint = 0;
        needed = 0;
        seen = 0;
      }
    }
    if (needed != 0) {
      out.append('�');
    }
    return out.toString();
  }

  /** Encodes {@code text}; each lone surrogate becomes the bytes of U+FFFD. */
  static byte[] encode(String text) {
    ByteArrayOutputStream out = new ByteArrayOutputStream(text.length() + 16);
    int length = text.length();
    for (int i = 0; i < length; i++) {
      int c = text.charAt(i);
      if (c >= 0xD800 && c <= 0xDBFF && i + 1 < length) {
        char low = text.charAt(i + 1);
        if (Character.isLowSurrogate(low)) {
          c = Character.toCodePoint((char) c, low);
          i++;
        }
      }
      if (c >= 0xD800 && c <= 0xDFFF) {
        c = 0xFFFD;
      }
      if (c < 0x80) {
        out.write(c);
      } else if (c < 0x800) {
        out.write(0xC0 | (c >> 6));
        out.write(0x80 | (c & 0x3F));
      } else if (c < 0x10000) {
        out.write(0xE0 | (c >> 12));
        out.write(0x80 | ((c >> 6) & 0x3F));
        out.write(0x80 | (c & 0x3F));
      } else {
        out.write(0xF0 | (c >> 18));
        out.write(0x80 | ((c >> 12) & 0x3F));
        out.write(0x80 | ((c >> 6) & 0x3F));
        out.write(0x80 | (c & 0x3F));
      }
    }
    return out.toByteArray();
  }
}
