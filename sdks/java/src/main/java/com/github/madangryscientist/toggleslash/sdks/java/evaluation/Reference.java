package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/** An attribute reference, to "Reading the attribute" in {@code docs/evaluation.md}. */
final class Reference {
  private Reference() {}

  /**
   * The components of a name that starts with {@code /}: what is left after the first {@code /},
   * split on {@code /}, with {@code ~1} as {@code /} and {@code ~0} as {@code ~} in one pass. Null
   * when a component is empty or a {@code ~} is followed by anything but {@code 0} or {@code 1}.
   */
  static List<String> parse(String text) {
    List<String> components = new ArrayList<>();
    int start = 1;
    while (true) {
      int slash = text.indexOf('/', start);
      String part = slash < 0 ? text.substring(start) : text.substring(start, slash);
      String decoded = decode(part);
      if (decoded == null) {
        return null;
      }
      components.add(decoded);
      if (slash < 0) {
        return components;
      }
      start = slash + 1;
    }
  }

  private static String decode(String part) {
    if (part.isEmpty()) {
      return null;
    }
    StringBuilder out = new StringBuilder(part.length());
    for (int i = 0; i < part.length(); i++) {
      char c = part.charAt(i);
      if (c != '~') {
        out.append(c);
        continue;
      }
      char next = i + 1 < part.length() ? part.charAt(i + 1) : '\0';
      if (next == '0') {
        out.append('~');
      } else if (next == '1') {
        out.append('/');
      } else {
        return null;
      }
      i++;
    }
    return out.toString();
  }
}
