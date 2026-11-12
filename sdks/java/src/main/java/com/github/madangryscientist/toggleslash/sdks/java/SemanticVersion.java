package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.List;

/**
 * A semantic version, read by a hand-written walk over the characters. Numeric parts stay strings
 * and compare by length and then by code unit, so they have no size limit and are never converted.
 */
final class SemanticVersion {
  private static final String[] NONE = new String[0];

  private final String major;
  private final String minor;
  private final String patch;
  private final String[] prerelease;

  private SemanticVersion(String major, String minor, String patch, String[] prerelease) {
    this.major = major;
    this.minor = minor;
    this.patch = patch;
    this.prerelease = prerelease;
  }

  /**
   * Parses a version.
   *
   * @param text the candidate
   * @return the version, or null when {@code text} is not one
   */
  static SemanticVersion parse(String text) {
    String core = text;
    String[] prerelease = NONE;
    int plus = core.indexOf('+');
    if (plus >= 0) {
      if (identifiers(core.substring(plus + 1)) == null) {
        return null;
      }
      core = core.substring(0, plus);
    }
    int dash = core.indexOf('-');
    if (dash >= 0) {
      String[] parts = identifiers(core.substring(dash + 1));
      if (parts == null) {
        return null;
      }
      for (String part : parts) {
        if (allDigits(part) && !isNumericPart(part)) {
          return null;
        }
      }
      prerelease = parts;
      core = core.substring(0, dash);
    }
    String[] numbers = splitOnDots(core);
    if (numbers.length < 3 && (plus >= 0 || dash >= 0)) {
      return null;
    }
    if (numbers.length > 3) {
      return null;
    }
    for (String number : numbers) {
      if (!isNumericPart(number)) {
        return null;
      }
    }
    return new SemanticVersion(
        numbers[0],
        numbers.length > 1 ? numbers[1] : "0",
        numbers.length > 2 ? numbers[2] : "0",
        prerelease);
  }

  /**
   * Orders two versions by SemVer 2.0.0 section 11; build metadata is already gone.
   *
   * @param other the version to compare with
   * @return -1, 0 or 1
   */
  int compareTo(SemanticVersion other) {
    int core = compareNumeric(major, other.major);
    if (core == 0) {
      core = compareNumeric(minor, other.minor);
    }
    if (core == 0) {
      core = compareNumeric(patch, other.patch);
    }
    if (core != 0) {
      return core;
    }
    if (prerelease.length == 0 || other.prerelease.length == 0) {
      if (prerelease.length == other.prerelease.length) {
        return 0;
      }
      return prerelease.length == 0 ? 1 : -1;
    }
    int shared = Math.min(prerelease.length, other.prerelease.length);
    for (int i = 0; i < shared; i++) {
      int order = compareIdentifier(prerelease[i], other.prerelease[i]);
      if (order != 0) {
        return order;
      }
    }
    return Integer.signum(Integer.compare(prerelease.length, other.prerelease.length));
  }

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  private static boolean isIdentifierChar(char c) {
    return isDigit(c) || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-';
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

  /** Splits on every {@code .}, keeping empty pieces (no platform regex is used). */
  private static String[] splitOnDots(String text) {
    List<String> pieces = new ArrayList<>();
    int start = 0;
    for (int i = text.indexOf('.'); i >= 0; i = text.indexOf('.', start)) {
      pieces.add(text.substring(start, i));
      start = i + 1;
    }
    pieces.add(text.substring(start));
    return pieces.toArray(new String[0]);
  }

  /** Dot-separated, none empty, each of ASCII letters, digits and {@code -}. */
  private static String[] identifiers(String text) {
    String[] parts = splitOnDots(text);
    for (String part : parts) {
      if (part.isEmpty()) {
        return null;
      }
      for (int i = 0; i < part.length(); i++) {
        if (!isIdentifierChar(part.charAt(i))) {
          return null;
        }
      }
    }
    return parts;
  }

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
}
