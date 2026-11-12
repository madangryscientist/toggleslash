package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/**
 * A semantic version, to the "Semantic versions" section of the spec. Numeric parts stay strings.
 */
final class Semver {
  final String major;
  final String minor;
  final String patch;
  final List<String> prerelease;

  private Semver(String major, String minor, String patch, List<String> prerelease) {
    this.major = major;
    this.minor = minor;
    this.patch = patch;
    this.prerelease = prerelease;
  }

  private static boolean isIdentifierChar(char c) {
    return Strings.isDigit(c) || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-';
  }

  /** {@code 0}, or digits with no leading zero. */
  private static boolean isNumericPart(String text) {
    return !text.isEmpty()
        && Strings.allDigits(text)
        && (text.equals("0") || text.charAt(0) != '0');
  }

  /** Dot-separated, none empty, each of ASCII letters, digits and a hyphen. */
  private static List<String> identifiers(String text) {
    List<String> parts = Strings.split(text, '.');
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

  private static List<String> prereleaseIdentifiers(String text) {
    List<String> parts = identifiers(text);
    if (parts == null) {
      return null;
    }
    for (String part : parts) {
      if (Strings.allDigits(part) && !isNumericPart(part)) {
        return null;
      }
    }
    return parts;
  }

  private static List<String> numericParts(String text, boolean allowShort) {
    List<String> numbers = Strings.split(text, '.');
    if (numbers.size() > 3 || (numbers.size() < 3 && !allowShort)) {
      return null;
    }
    for (String number : numbers) {
      if (!isNumericPart(number)) {
        return null;
      }
    }
    return numbers;
  }

  /** The version the text spells, or null when it is not one. */
  static Semver parse(String text) {
    int plus = text.indexOf('+');
    String withoutBuild = plus < 0 ? text : text.substring(0, plus);
    boolean hasBuild = plus >= 0;
    if (hasBuild && identifiers(text.substring(plus + 1)) == null) {
      return null;
    }
    int dash = withoutBuild.indexOf('-');
    String core = dash < 0 ? withoutBuild : withoutBuild.substring(0, dash);
    List<String> prerelease = new ArrayList<>();
    if (dash >= 0) {
      prerelease = prereleaseIdentifiers(withoutBuild.substring(dash + 1));
      if (prerelease == null) {
        return null;
      }
    }
    List<String> numbers = numericParts(core, !hasBuild && dash < 0);
    if (numbers == null) {
      return null;
    }
    return new Semver(
        numbers.get(0),
        numbers.size() > 1 ? numbers.get(1) : "0",
        numbers.size() > 2 ? numbers.get(2) : "0",
        prerelease);
  }

  private static int compareNumeric(String a, String b) {
    if (a.length() != b.length()) {
      return a.length() < b.length() ? -1 : 1;
    }
    return Integer.signum(a.compareTo(b));
  }

  private static int compareIdentifier(String a, String b) {
    boolean aNumeric = Strings.allDigits(a);
    boolean bNumeric = Strings.allDigits(b);
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

  /** -1, 0 or 1 by SemVer 2.0.0 section 11. */
  int compareTo(Semver other) {
    int core = compareNumeric(major, other.major);
    if (core == 0) {
      core = compareNumeric(minor, other.minor);
    }
    if (core == 0) {
      core = compareNumeric(patch, other.patch);
    }
    return core != 0 ? core : comparePrerelease(prerelease, other.prerelease);
  }
}
