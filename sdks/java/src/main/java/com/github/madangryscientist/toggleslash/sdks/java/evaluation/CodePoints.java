package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** The code points of a UTF-16 string, where a lone surrogate is its own code point. */
final class CodePoints {
  private CodePoints() {}

  static int[] of(String text) {
    int[] points = new int[text.length()];
    int count = 0;
    for (int i = 0; i < text.length(); i++) {
      char high = text.charAt(i);
      if (Character.isHighSurrogate(high)
          && i + 1 < text.length()
          && Character.isLowSurrogate(text.charAt(i + 1))) {
        points[count++] = Character.toCodePoint(high, text.charAt(i + 1));
        i++;
      } else {
        points[count++] = high;
      }
    }
    return java.util.Arrays.copyOf(points, count);
  }
}
