package com.github.madangryscientist.toggleslash.sdks.java.eval;

import java.util.ArrayList;
import java.util.List;

/**
 * Semantic versions, to the "Semantic versions" section of {@code docs/evaluation.md}: a
 * hand-written walk over the string. Nothing here converts a numeric part to a number.
 */
final class Semver {
  final String major;
  final String minor;
  final String patch;

  /** The dot-separated prerelease identifiers; empty when there is no prerelease. */
  final List<String> prerelease;

  private Semver(String major, String minor, String patch, List<String> prerelease) {
    this.major = major;
    this.minor = minor;
    this.patch = patch;
    this.prerelease = prerelease;
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

  /** Zero, or digits with no leading zero. */
  private static boolean isNumericPart(String text) {
    return !text.isEmpty() && allDigits(text) && (text.equals("0") || text.charAt(0) != '0');
  }

  /** Dot-separated, none empty, each of ASCII letters, digits and a hyphen. */
  private static List<String> identifiers(String text) {
    List<String> parts = splitOnDots(text);
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

  private static List<String> splitOnDots(String text) {
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

  private static List<String> prereleaseIdentifiers(String text) {
    List<String> parts = identifiers(text);
    if (parts == null) {
      return null;
    }
    for (String part : parts) {
      if (allDigits(part) && !isNumericPart(part)) {
        return null;
      }
    }
    return parts;
  }

  private static List<String> numericParts(String text, boolean allowShort) {
    List<String> numbers = splitOnDots(text);
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

  /**
   * The version a string spells.
   *
   * @param text the candidate
   * @return the version, or null when it is not one
   */
  static Semver parse(String text) {
    int plus = text.indexOf('+');
    String withoutBuild = plus < 0 ? text : text.substring(0, plus);
    String build = plus < 0 ? null : text.substring(plus + 1);
    // Build metadata: identifiers only, and leading zeros are fine.
    if (build != null && identifiers(build) == null) {
      return null;
    }
    int dash = withoutBuild.indexOf('-');
    String core = dash < 0 ? withoutBuild : withoutBuild.substring(0, dash);
    String prerelease = dash < 0 ? null : withoutBuild.substring(dash + 1);
    List<String> parsedPrerelease = List.of();
    if (prerelease != null) {
      parsedPrerelease = prereleaseIdentifiers(prerelease);
      if (parsedPrerelease == null) {
        return null;
      }
    }
    // The short forms carry no suffix.
    List<String> numbers = numericParts(core, build == null && prerelease == null);
    if (numbers == null) {
      return null;
    }
    return new Semver(
        numbers.get(0),
        numbers.size() > 1 ? numbers.get(1) : "0",
        numbers.size() > 2 ? numbers.get(2) : "0",
        parsedPrerelease);
  }

  private static int sign(int order) {
    return Integer.signum(order);
  }

  /** Digit strings with no leading zero: the longer is larger, else by code unit. */
  private static int compareNumeric(String a, String b) {
    if (a.length() != b.length()) {
      return a.length() < b.length() ? -1 : 1;
    }
    return sign(a.compareTo(b));
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
    return sign(a.compareTo(b));
  }

  private static int comparePrerelease(List<String> a, List<String> b) {
    // No prerelease is above any prerelease of the same core.
    if (a.isEmpty() || b.isEmpty()) {
      return sign(b.size() - a.size());
    }
    int shared = Math.min(a.size(), b.size());
    for (int i = 0; i < shared; i++) {
      int order = compareIdentifier(a.get(i), b.get(i));
      if (order != 0) {
        return order;
      }
    }
    return sign(a.size() - b.size());
  }

  /**
   * Compares by SemVer 2.0.0 section 11; build metadata is already gone.
   *
   * @param a one version
   * @param b the other
   * @return -1, 0 or 1
   */
  static int compare(Semver a, Semver b) {
    int core = compareNumeric(a.major, b.major);
    if (core == 0) {
      core = compareNumeric(a.minor, b.minor);
    }
    if (core == 0) {
      core = compareNumeric(a.patch, b.patch);
    }
    return core != 0 ? core : comparePrerelease(a.prerelease, b.prerelease);
  }
}
