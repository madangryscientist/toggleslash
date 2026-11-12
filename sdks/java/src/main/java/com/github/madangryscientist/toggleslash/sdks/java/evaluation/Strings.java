package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/** Small string helpers that read the same on every JDK and use no regular expression. */
final class Strings {
  private Strings() {}

  /** Splits on a character, keeping empty parts, including the trailing ones. */
  static List<String> split(String text, char separator) {
    List<String> parts = new ArrayList<>();
    int start = 0;
    while (true) {
      int at = text.indexOf(separator, start);
      if (at < 0) {
        parts.add(text.substring(start));
        return parts;
      }
      parts.add(text.substring(start, at));
      start = at + 1;
    }
  }

  static boolean isDigit(int code) {
    return code >= '0' && code <= '9';
  }

  static boolean allDigits(String text) {
    for (int i = 0; i < text.length(); i++) {
      if (!isDigit(text.charAt(i))) {
        return false;
      }
    }
    return true;
  }
}
