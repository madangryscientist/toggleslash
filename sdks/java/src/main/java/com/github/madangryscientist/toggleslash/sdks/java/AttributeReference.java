package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.List;

/**
 * An attribute reference, a name that starts with {@code /}: the components after dropping the
 * slash, splitting on {@code /} and decoding {@code ~1} as {@code /} and {@code ~0} as {@code ~} in
 * one pass.
 */
final class AttributeReference {
  private AttributeReference() {}

  /**
   * Parses a reference.
   *
   * @param text a name that starts with {@code /}
   * @return its components, or null when one is empty or a {@code ~} is not followed by 0 or 1
   */
  static List<String> parse(String text) {
    List<String> components = new ArrayList<>();
    int length = text.length();
    int start = 1;
    while (true) {
      int end = text.indexOf('/', start);
      if (end < 0) {
        end = length;
      }
      if (end == start) {
        return null;
      }
      StringBuilder decoded = new StringBuilder();
      for (int i = start; i < end; i++) {
        char c = text.charAt(i);
        if (c != '~') {
          decoded.append(c);
          continue;
        }
        if (i + 1 >= end) {
          return null;
        }
        char next = text.charAt(i + 1);
        if (next != '0' && next != '1') {
          return null;
        }
        decoded.append(next == '0' ? '~' : '/');
        i++;
      }
      components.add(decoded.toString());
      if (end == length) {
        return components;
      }
      start = end + 1;
    }
  }
}
