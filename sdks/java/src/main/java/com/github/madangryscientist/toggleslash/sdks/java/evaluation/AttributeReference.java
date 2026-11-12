package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * An attribute reference: a name that starts with a slash. The components are what is left after
 * dropping the slash, splitting on slashes and decoding {@code ~1} as a slash and {@code ~0} as a
 * tilde in one pass.
 */
final class AttributeReference {
  private AttributeReference() {}

  /**
   * Splits a reference.
   *
   * @param text a name starting with a slash
   * @return the components, or null when one is empty or a tilde is not followed by 0 or 1
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
      StringBuilder decoded = new StringBuilder(part.length());
      for (int i = 0; i < part.length(); i++) {
        char c = part.charAt(i);
        if (c != '~') {
          decoded.append(c);
          continue;
        }
        if (i + 1 >= part.length() || (part.charAt(i + 1) != '0' && part.charAt(i + 1) != '1')) {
          return null;
        }
        decoded.append(part.charAt(i + 1) == '0' ? '~' : '/');
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
