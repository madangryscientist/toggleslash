package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * Instants, to the evaluation spec: a hand-written walk over the string, no {@code java.time}
 * parser, so the calendar and the strictness are the spec's.
 */
final class Instants {

  private Instants() {}

  /**
   * A finite number is Unix milliseconds, a string is parsed, anything else is not an instant.
   * Returns NaN for "not an instant".
   */
  static double of(Object value) {
    if (value instanceof Double d) {
      return Double.isFinite(d) ? d : Double.NaN;
    }
    if (value instanceof Number n) {
      double d = n.doubleValue();
      return Double.isFinite(d) ? d : Double.NaN;
    }
    return value instanceof String s ? parse(s) : Double.NaN;
  }

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  /** {@code length} ASCII digits at {@code start} as a number, or -1. */
  private static int digits(String text, int start, int length) {
    if (start + length > text.length()) {
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
    long y = month <= 2 ? year - 1L : year;
    long era = Math.floorDiv(y, 400L);
    long yearOfEra = y - era * 400;
    long monthIndex = month > 2 ? month - 3 : month + 9;
    long dayOfYear = (153 * monthIndex + 2) / 5 + day - 1;
    long dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear;
    return era * 146097 + dayOfEra - 719468;
  }

  /** {@code YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)} as Unix milliseconds, or NaN. */
  static double parse(String text) {
    boolean shape =
        text.length() >= 20
            && text.charAt(4) == '-'
            && text.charAt(7) == '-'
            && text.charAt(10) == 'T'
            && text.charAt(13) == ':'
            && text.charAt(16) == ':';
    if (!shape) {
      return Double.NaN;
    }
    int year = digits(text, 0, 4);
    int month = digits(text, 5, 2);
    int day = digits(text, 8, 2);
    int hour = digits(text, 11, 2);
    int minute = digits(text, 14, 2);
    int second = digits(text, 17, 2);
    if (year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month)) {
      return Double.NaN;
    }
    if (hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59) {
      return Double.NaN;
    }
    int end = 19;
    int fractionMs = 0;
    if (text.charAt(19) == '.') {
      end = 20;
      while (end < text.length() && isDigit(text.charAt(end))) {
        end++;
      }
      int count = end - 20;
      if (count == 0) {
        return Double.NaN;
      }
      int kept = Math.min(count, 3);
      int scale = kept == 1 ? 100 : kept == 2 ? 10 : 1;
      fractionMs = digits(text, 20, kept) * scale;
    }
    String offset = text.substring(end);
    long offsetMinutes;
    if (offset.equals("Z")) {
      offsetMinutes = 0;
    } else {
      int sign =
          offset.isEmpty() ? 0 : offset.charAt(0) == '+' ? 1 : offset.charAt(0) == '-' ? -1 : 0;
      if (sign == 0 || offset.length() != 6 || offset.charAt(3) != ':') {
        return Double.NaN;
      }
      int offsetHour = digits(offset, 1, 2);
      int offsetMinute = digits(offset, 4, 2);
      if (offsetHour < 0 || offsetHour > 23 || offsetMinute < 0 || offsetMinute > 59) {
        return Double.NaN;
      }
      offsetMinutes = (long) sign * (offsetHour * 60L + offsetMinute);
    }
    long seconds = (hour * 60L + minute) * 60 + second;
    long millis =
        daysFromCivil(year, month, day) * 86_400_000L
            + seconds * 1000
            + fractionMs
            - offsetMinutes * 60_000L;
    return (double) millis;
  }
}
