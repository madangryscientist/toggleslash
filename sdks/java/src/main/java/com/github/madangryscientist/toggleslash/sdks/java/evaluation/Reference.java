package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/** An attribute reference: a name that starts with a slash. */
final class Reference {
  private Reference() {}

  /**
   * The components after dropping the leading slash, splitting on slashes and decoding {@code ~1}
   * as a slash and {@code ~0} as a tilde in one left-to-right pass. Null when a component is empty
   * or a tilde is followed by anything but 0 or 1, or ends the component.
   */
  static List<String> parse(String text) {
    List<String> components = new ArrayList<>();
    for (String part : Strings.split(text.substring(1), '/')) {
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
