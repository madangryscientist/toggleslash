package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/** An attribute reference: a name that starts with a slash. */
final class Reference {
  private Reference() {}

  /**
   * The components of a reference: the text after the leading slash, split on slashes, with {@code
   * ~1} decoded as a slash and {@code ~0} as a tilde in one left-to-right pass.
   *
   * @return the components, or {@code null} when one is empty or a tilde is followed by anything
   *     but 0 or 1, or ends the component
   */
  static List<String> parse(String text) {
    List<String> components = new ArrayList<>();
    String rest = text.substring(1);
    int start = 0;
    while (start <= rest.length()) {
      int slash = rest.indexOf('/', start);
      int end = slash < 0 ? rest.length() : slash;
      String part = rest.substring(start, end);
      start = end + 1;
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
