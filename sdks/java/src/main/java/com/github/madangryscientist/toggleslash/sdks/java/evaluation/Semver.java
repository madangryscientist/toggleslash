package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.List;

/**
 * A semantic version, parsed by a walk over the characters. Numeric parts stay strings and compare
 * by length and then by code unit, so they have no size limit. The short forms {@code MAJOR} and
 * {@code MAJOR.MINOR} are accepted and carry no suffix.
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

  /**
   * Reads a version.
   *
   * @param text the text
   * @return the version, or null when the text is not one
   */
  static Semver parse(String text) {
    String core = text;
    List<String> prerelease = List.of();
    int plus = core.indexOf('+');
    if (plus >= 0) {
      if (identifiers(core.substring(plus + 1)) == null) {
        return null;
      }
      core = core.substring(0, plus);
    }
    int dash = core.indexOf('-');
    if (dash >= 0) {
      List<String> parts = identifiers(core.substring(dash + 1));
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
    String[] numbers = splitDots(core);
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
    return new Semver(
        numbers[0],
        numbers.length > 1 ? numbers[1] : "0",
        numbers.length > 2 ? numbers[2] : "0",
        prerelease);
  }

  /**
   * Orders two versions by SemVer 2.0.0 section 11; build metadata is already gone.
   *
   * @param other the other version
   * @return -1, 0 or 1
   */
  int compareTo(Semver other) {
    int order = compareNumeric(major, other.major);
    if (order == 0) {
      order = compareNumeric(minor, other.minor);
    }
    if (order == 0) {
      order = compareNumeric(patch, other.patch);
    }
    if (order != 0) {
      return order;
    }
    if (prerelease.isEmpty() || other.prerelease.isEmpty()) {
      if (prerelease.size() == other.prerelease.size()) {
        return 0;
      }
      return prerelease.isEmpty() ? 1 : -1;
    }
    int shared = Math.min(prerelease.size(), other.prerelease.size());
    for (int i = 0; i < shared; i++) {
      int byIdentifier = compareIdentifier(prerelease.get(i), other.prerelease.get(i));
      if (byIdentifier != 0) {
        return byIdentifier;
      }
    }
    return Integer.signum(Integer.compare(prerelease.size(), other.prerelease.size()));
  }

  /** Splits on dots, keeping empty parts, without a regular expression. */
  private static String[] splitDots(String text) {
    java.util.ArrayList<String> parts = new java.util.ArrayList<>();
    int start = 0;
    for (int i = 0; i <= text.length(); i++) {
      if (i == text.length() || text.charAt(i) == '.') {
        parts.add(text.substring(start, i));
        start = i + 1;
      }
    }
    return parts.toArray(new String[0]);
  }

  private static boolean allDigits(String text) {
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      if (c < '0' || c > '9') {
        return false;
      }
    }
    return true;
  }

  private static boolean isNumericPart(String text) {
    return !text.isEmpty() && allDigits(text) && (text.equals("0") || text.charAt(0) != '0');
  }

  private static boolean isIdentifierChar(char c) {
    return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-';
  }

  private static List<String> identifiers(String text) {
    String[] parts = splitDots(text);
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
    return List.of(parts);
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
