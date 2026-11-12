package com.github.madangryscientist.toggleslash.sdks.java.eval;

import java.util.ArrayList;
import java.util.List;

/** An attribute reference: a name that starts with a slash, to "Reading the attribute". */
final class Reference {
  private Reference() {}

  /**
   * The components of a reference: what is left after dropping the leading slash, splitting on
   * slashes and decoding {@code ~1} as a slash and {@code ~0} as a tilde in one left-to-right pass.
   *
   * @param text a name that starts with a slash
   * @return the components, or null when one is empty or a tilde is followed by anything but 0 or 1
   */
  static List<String> parse(String text) {
    List<String> components = new ArrayList<>();
    int start = 1;
    while (true) {
      int slash = text.indexOf('/', start);
      String part = slash < 0 ? text.substring(start) : text.substring(start, slash);
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
      if (slash < 0) {
        return components;
      }
      start = slash + 1;
    }
  }
}
