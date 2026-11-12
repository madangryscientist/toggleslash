package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/** Instants, to the "Dates" section of the evaluation spec: a hand-written walk over the string. */
final class Dates {
  private Dates() {}

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  /** {@code length} ASCII digits at {@code start} as a number, or -1. */
  private static int digits(String text, int start, int length) {
    if (start < 0 || start + length > text.length()) {
      return -1;
    }
    int value = 0;
    for (int i = start; i < start + length; i++) {
      char c = text.charAt(i);
      if (!isDigit(c)) {
        return -1;
      }
      value = value * 10 + (c - '0');
    }
    return value;
  }

  private static boolean isLeapYear(int year) {
    return year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
  }

  private static int daysInMonth(int year, int month) {
    if (month == 2) {
      return isLeapYear(year) ? 29 : 28;
    }
    return month == 4 || month == 6 || month == 9 || month == 11 ? 30 : 31;
  }

  /** Days since 1970-01-01 in the proleptic Gregorian calendar. */
  private static long daysFromCivil(int year, int month, int day) {
    int y = month <= 2 ? year - 1 : year;
    int era = Math.floorDiv(y, 400);
    int yearOfEra = y - era * 400;
    int monthIndex = month > 2 ? month - 3 : month + 9;
    int dayOfYear = (153 * monthIndex + 2) / 5 + day - 1;
    int dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear;
    return (long) era * 146097 + dayOfEra - 719468;
  }

  private static boolean hasShape(String text) {
    return text.length() >= 20
        && text.charAt(4) == '-'
        && text.charAt(7) == '-'
        && text.charAt(10) == 'T'
        && text.charAt(13) == ':'
        && text.charAt(16) == ':';
  }

  /** The offset that is all of the text, in minutes east of UTC, or null. */
  private static Integer parseOffset(String text) {
    if (text.equals("Z")) {
      return 0;
    }
    if (text.length() != 6 || text.charAt(3) != ':') {
      return null;
    }
    int sign;
    if (text.charAt(0) == '+') {
      sign = 1;
    } else if (text.charAt(0) == '-') {
      sign = -1;
    } else {
      return null;
    }
    int hour = digits(text, 1, 2);
    int minute = digits(text, 4, 2);
    if (hour < 0 || minute < 0 || hour > 23 || minute > 59) {
      return null;
    }
    return sign * (hour * 60 + minute);
  }

  /** {@code YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)} as Unix milliseconds, or NaN. */
  static double parseInstant(String text) {
    if (!hasShape(text)) {
      return Double.NaN;
    }
    int year = digits(text, 0, 4);
    int month = digits(text, 5, 2);
    int day = digits(text, 8, 2);
    if (year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month)) {
      return Double.NaN;
    }
    int hour = digits(text, 11, 2);
    int minute = digits(text, 14, 2);
    int second = digits(text, 17, 2);
    if (hour < 0 || minute < 0 || second < 0 || hour > 23 || minute > 59 || second > 59) {
      return Double.NaN;
    }
    int end = 19;
    int millis = 0;
    if (text.charAt(19) == '.') {
      end = 20;
      while (end < text.length() && isDigit(text.charAt(end))) {
        end++;
      }
      if (end == 20) {
        return Double.NaN;
      }
      int kept = Math.min(end - 20, 3);
      int scale = kept == 1 ? 100 : kept == 2 ? 10 : 1;
      millis = digits(text, 20, kept) * scale;
    }
    Integer offset = parseOffset(text.substring(end));
    if (offset == null) {
      return Double.NaN;
    }
    long days = daysFromCivil(year, month, day);
    long seconds = (hour * 60L + minute) * 60 + second;
    return (double) (days * 86_400_000L + seconds * 1000 + millis - offset * 60_000L);
  }

  /** A finite number is Unix milliseconds, a string is parsed, anything else is NaN. */
  static double toInstant(Object value) {
    if (value instanceof Double) {
      double d = (Double) value;
      return Double.isFinite(d) ? d : Double.NaN;
    }
    return value instanceof String ? parseInstant((String) value) : Double.NaN;
  }
}
