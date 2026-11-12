package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.List;

/** Semantic versions, to the "Semantic versions" section of the evaluation spec. */
final class Semver {
  /** A parsed version. The numeric parts stay strings, so none can overflow. */
  static final class Version {
    final String major;
    final String minor;
    final String patch;
    final List<String> prerelease;

    Version(String major, String minor, String patch, List<String> prerelease) {
      this.major = major;
      this.minor = minor;
      this.patch = patch;
      this.prerelease = prerelease;
    }
  }

  private Semver() {}

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

  private static boolean isNumericPart(String text) {
    return !text.isEmpty() && allDigits(text) && (text.equals("0") || text.charAt(0) != '0');
  }

  private static List<String> splitDots(String text) {
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

  private static List<String> identifiers(String text) {
    List<String> parts = splitDots(text);
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
      if (allDigits(part) && !isNumericPart(part)) {
        return null;
      }
    }
    return parts;
  }

  private static List<String> numericParts(String text, boolean allowShort) {
    List<String> numbers = splitDots(text);
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

  /** The version the text spells, or {@code null} when it is not one. */
  static Version parse(String text) {
    String withoutBuild = text;
    String build = null;
    int plus = text.indexOf('+');
    if (plus >= 0) {
      withoutBuild = text.substring(0, plus);
      build = text.substring(plus + 1);
    }
    if (build != null && identifiers(build) == null) {
      return null;
    }
    String core = withoutBuild;
    String prerelease = null;
    int dash = withoutBuild.indexOf('-');
    if (dash >= 0) {
      core = withoutBuild.substring(0, dash);
      prerelease = withoutBuild.substring(dash + 1);
    }
    List<String> parsedPrerelease = List.of();
    if (prerelease != null) {
      parsedPrerelease = prereleaseIdentifiers(prerelease);
      if (parsedPrerelease == null) {
        return null;
      }
    }
    List<String> numbers = numericParts(core, build == null && prerelease == null);
    if (numbers == null) {
      return null;
    }
    return new Version(
        numbers.get(0),
        numbers.size() > 1 ? numbers.get(1) : "0",
        numbers.size() > 2 ? numbers.get(2) : "0",
        parsedPrerelease);
  }

  private static int sign(int order) {
    return Integer.compare(order, 0);
  }

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

  /** -1, 0 or 1 by SemVer 2.0.0 section 11. */
  static int compare(Version a, Version b) {
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
