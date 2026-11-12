package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * Dates to {@code docs/evaluation.md}: a strict RFC 3339 reader, and numbers as Unix milliseconds.
 */
public final class Dates {
  private Dates() {}

  /**
   * The instant of an attribute or clause value, in Unix milliseconds.
   *
   * @param value a finite number, or a string in {@code YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)}
   * @return the instant, or null when the value is not a date
   */
  public static Double toInstant(Object value) {
    if (value instanceof Double d) {
      return d.isNaN() || d.isInfinite() ? null : d;
    }
    if (value instanceof String s) {
      return parse(s);
    }
    return null;
  }

  private static boolean digit(char c) {
    return c >= '0' && c <= '9';
  }

  private static int number(String s, int from, int length) {
    int v = 0;
    for (int i = from; i < from + length; i++) {
      if (!digit(s.charAt(i))) {
        return -1;
      }
      v = v * 10 + (s.charAt(i) - '0');
    }
    return v;
  }

  private static Double parse(String s) {
    if (s.length() < 20
        || s.charAt(4) != '-'
        || s.charAt(7) != '-'
        || s.charAt(10) != 'T'
        || s.charAt(13) != ':'
        || s.charAt(16) != ':') {
      return null;
    }
    int year = number(s, 0, 4);
    int month = number(s, 5, 2);
    int day = number(s, 8, 2);
    int hour = number(s, 11, 2);
    int minute = number(s, 14, 2);
    int second = number(s, 17, 2);
    if (year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month)) {
      return null;
    }
    if (hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59) {
      return null;
    }
    int index = 19;
    long fractionMs = 0;
    if (s.charAt(index) == '.') {
      int start = index + 1;
      int end = start;
      while (end < s.length() && digit(s.charAt(end))) {
        end++;
      }
      if (end == start) {
        return null;
      }
      int kept = Math.min(3, end - start);
      int v = number(s, start, kept);
      fractionMs = v * new int[] {1, 100, 10, 1}[kept];
      index = end;
    }
    long offsetMinutes;
    if (index >= s.length()) {
      return null;
    }
    char z = s.charAt(index);
    if (z == 'Z' && index == s.length() - 1) {
      offsetMinutes = 0;
    } else if ((z == '+' || z == '-') && s.length() - index == 6 && s.charAt(index + 3) == ':') {
      int oh = number(s, index + 1, 2);
      int om = number(s, index + 4, 2);
      if (oh < 0 || oh > 23 || om < 0 || om > 59) {
        return null;
      }
      offsetMinutes = (oh * 60L + om) * (z == '-' ? -1 : 1);
    } else {
      return null;
    }
    long days = daysFromCivil(year, month, day);
    long seconds = hour * 3600L + minute * 60L + second;
    return (double) (days * 86400000L + seconds * 1000L + fractionMs - offsetMinutes * 60000L);
  }

  private static boolean leap(int year) {
    return (year % 4 == 0 && year % 100 != 0) || year % 400 == 0;
  }

  private static int daysInMonth(int year, int month) {
    switch (month) {
      case 2:
        return leap(year) ? 29 : 28;
      case 4:
      case 6:
      case 9:
      case 11:
        return 30;
      default:
        return 31;
    }
  }

  private static long daysFromCivil(int year, int month, int day) {
    long y = month <= 2 ? year - 1 : year;
    long era = Math.floorDiv(y, 400L);
    long yearOfEra = y - era * 400;
    long monthIndex = (month + 9) % 12;
    long dayOfYear = (153 * monthIndex + 2) / 5 + day - 1;
    long dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear;
    return era * 146097 + dayOfEra - 719468;
  }
}
