package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * Semantic versions, to the evaluation spec: a hand-written walk over the characters, no regular
 * expression, and no numeric part is ever converted to a number.
 */
final class Semver {

  private final String major;
  private final String minor;
  private final String patch;
  private final List<String> prerelease;

  private Semver(String major, String minor, String patch, List<String> prerelease) {
    this.major = major;
    this.minor = minor;
    this.patch = patch;
    this.prerelease = prerelease;
  }

  /** The version {@code text} spells, or null when it is not one. */
  static Semver parse(String text) {
    int plus = text.indexOf('+');
    String withoutBuild = plus < 0 ? text : text.substring(0, plus);
    if (plus >= 0 && identifiers(text.substring(plus + 1)) == null) {
      return null;
    }
    int dash = withoutBuild.indexOf('-');
    String core = dash < 0 ? withoutBuild : withoutBuild.substring(0, dash);
    List<String> pre = List.of();
    if (dash >= 0) {
      List<String> parts = identifiers(withoutBuild.substring(dash + 1));
      if (parts == null) {
        return null;
      }
      for (String part : parts) {
        if (allDigits(part) && !isNumericPart(part)) {
          return null;
        }
      }
      pre = parts;
    }
    boolean shortAllowed = plus < 0 && dash < 0;
    List<String> numbers = dotParts(core);
    if (numbers.size() > 3 || (numbers.size() < 3 && !shortAllowed)) {
      return null;
    }
    for (String number : numbers) {
      if (!isNumericPart(number)) {
        return null;
      }
    }
    return new Semver(
        numbers.get(0),
        numbers.size() > 1 ? numbers.get(1) : "0",
        numbers.size() > 2 ? numbers.get(2) : "0",
        pre);
  }

  /** -1, 0 or 1 by SemVer 2.0.0 section 11; build metadata is already gone. */
  int compareTo(Semver other) {
    int order = compareNumeric(major, other.major);
    if (order == 0) {
      order = compareNumeric(minor, other.minor);
    }
    if (order == 0) {
      order = compareNumeric(patch, other.patch);
    }
    return order != 0 ? order : comparePrerelease(prerelease, other.prerelease);
  }

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  private static boolean allDigits(String text) {
    for (int i = 0; i < text.length(); i++) {
      if (!isDigit(text.charAt(i))) {
        return false;
      }
    }
    return true;
  }

  /** {@code 0}, or digits with no leading zero. */
  private static boolean isNumericPart(String text) {
    return !text.isEmpty() && allDigits(text) && (text.equals("0") || text.charAt(0) != '0');
  }

  /** The pieces between dots, empty ones included. */
  private static List<String> dotParts(String text) {
    List<String> parts = new ArrayList<>();
    int start = 0;
    while (true) {
      int dot = text.indexOf('.', start);
      if (dot < 0) {
        parts.add(text.substring(start));
        return parts;
      }
      parts.add(text.substring(start, dot));
      start = dot + 1;
    }
  }

  /** Dot-separated, none empty, each of ASCII letters, digits and {@code -}. */
  private static List<String> identifiers(String text) {
    List<String> parts = new ArrayList<>();
    int start = 0;
    while (true) {
      int dot = text.indexOf('.', start);
      String part = dot < 0 ? text.substring(start) : text.substring(start, dot);
      if (part.isEmpty()) {
        return null;
      }
      for (int i = 0; i < part.length(); i++) {
        char c = part.charAt(i);
        boolean ok = isDigit(c) || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-';
        if (!ok) {
          return null;
        }
      }
      parts.add(part);
      if (dot < 0) {
        return parts;
      }
      start = dot + 1;
    }
  }

  /** Digit strings with no leading zero: the longer is larger, else by code unit. */
  private static int compareNumeric(String a, String b) {
    if (a.length() != b.length()) {
      return a.length() < b.length() ? -1 : 1;
    }
    return Integer.signum(a.compareTo(b));
  }

  private static int compareIdentifier(String a, String b) {
    boolean aNumeric = allDigits(a);
    boolean bNumeric = allDigits(b);
    if (aNumeric && bNumeric) {
      return compareNumeric(a, b);
    }
    if (aNumeric) {
      return -1;
    }
    if (bNumeric) {
      return 1;
    }
    return Integer.signum(a.compareTo(b));
  }

  private static int comparePrerelease(List<String> a, List<String> b) {
    if (a.isEmpty() || b.isEmpty()) {
      return Integer.signum(b.size() - a.size());
    }
    int shared = Math.min(a.size(), b.size());
    for (int i = 0; i < shared; i++) {
      int order = compareIdentifier(a.get(i), b.get(i));
      if (order != 0) {
        return order;
      }
    }
    return Integer.signum(a.size() - b.size());
  }
}
