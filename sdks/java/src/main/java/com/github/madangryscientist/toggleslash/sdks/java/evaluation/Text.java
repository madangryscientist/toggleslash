package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/** String helpers that stand in for the platform calls that take a regular expression. */
final class Text {
  private Text() {}

  /**
   * Splits on one character, keeping empty parts, as {@code split(separator, -1)} does.
   *
   * @param text the text
   * @param separator the character to split on
   * @return the parts, at least one
   */
  static List<String> split(String text, char separator) {
    List<String> parts = new ArrayList<>();
    int from = 0;
    for (int at = text.indexOf(separator); at >= 0; at = text.indexOf(separator, from)) {
      parts.add(text.substring(from, at));
      from = at + 1;
    }
    parts.add(text.substring(from));
    return parts;
  }
}
