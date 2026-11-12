package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * An attribute reference, to "Reading the attribute" in docs/evaluation.md: a name that starts with
 * a slash. The components are what is left after dropping the leading slash, splitting on slashes
 * and decoding {@code ~1} as a slash and {@code ~0} as a tilde in one left-to-right pass.
 */
final class Reference {
  private Reference() {}

  /** The components, or null when one is empty or a tilde is followed by anything but 0 or 1. */
  static List<String> parse(String text) {
    List<String> components = new ArrayList<>();
    for (String part : Text.split(text.substring(1), '/')) {
      if (part.isEmpty()) {
        return null;
      }
      StringBuilder decoded = new StringBuilder();
      for (int i = 0; i < part.length(); i++) {
        char c = part.charAt(i);
        if (c != '~') {
          decoded.append(c);
          continue;
        }
        char next = i + 1 < part.length() ? part.charAt(i + 1) : '\0';
        if (next == '0') {
          decoded.append('~');
        } else if (next == '1') {
          decoded.append('/');
        } else {
          return null;
        }
        i++;
      }
      components.add(decoded.toString());
    }
    return components;
  }
}
