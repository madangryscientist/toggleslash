package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * Instants, to the "Dates" section of the spec. A hand-written walk over the string: no regular
 * expression, no {@code java.time} parsing, so the strict rules are exactly the spec's.
 */
final class Dates {
  private Dates() {}

  /** {@code length} ASCII digits at {@code start} as a number; -1 when any is not a digit. */
  private static int digits(String text, int start, int length) {
    int value = 0;
    for (int i = start; i < start + length; i++) {
      char c = text.charAt(i);
      if (!Strings.isDigit(c)) {
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

  /** Days since 1970-01-01 in the proleptic Gregorian calendar ("days from civil"). */
  private static long daysFromCivil(int year, int month, int day) {
    int y = month <= 2 ? year - 1 : year;
    int era = Math.floorDiv(y, 400);
    int yearOfEra = y - era * 400;
    int monthIndex = month > 2 ? month - 3 : month + 9;
    int dayOfYear = (153 * monthIndex + 2) / 5 + day - 1;
    int dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear;
    return (long) era * 146097 + dayOfEra - 719468;
  }

  /** The days since 1970-01-01 of the date at the start of the text, or null. */
  private static Long parseDate(String text) {
    int year = digits(text, 0, 4);
    int month = digits(text, 5, 2);
    int day = digits(text, 8, 2);
    if (year < 1 || month < 1 || month > 12 || day < 1) {
      return null;
    }
    if (day > daysInMonth(year, month)) {
      return null;
    }
    return daysFromCivil(year, month, day);
  }

  /** The seconds since midnight of {@code HH:MM:SS} at {@code start}, or -1. */
  private static int parseTime(String text, int start) {
    int hour = digits(text, start, 2);
    int minute = digits(text, start + 3, 2);
    int second = digits(text, start + 6, 2);
    if (hour < 0 || minute < 0 || second < 0 || hour > 23 || minute > 59 || second > 59) {
      return -1;
    }
    return (hour * 60 + minute) * 60 + second;
  }

  /**
   * The offset that is all of the text from {@code start}, in minutes east of UTC; null otherwise.
   */
  private static Integer parseOffset(String text, int start) {
    int length = text.length() - start;
    if (length == 1 && text.charAt(start) == 'Z') {
      return 0;
    }
    if (length != 6) {
      return null;
    }
    char sign = text.charAt(start);
    if ((sign != '+' && sign != '-') || text.charAt(start + 3) != ':') {
      return null;
    }
    int hour = digits(text, start + 1, 2);
    int minute = digits(text, start + 4, 2);
    if (hour < 0 || minute < 0 || hour > 23 || minute > 59) {
      return null;
    }
    int minutes = hour * 60 + minute;
    return sign == '+' ? minutes : -minutes;
  }

  private static boolean hasShape(String text) {
    return text.length() >= 20
        && text.charAt(4) == '-'
        && text.charAt(7) == '-'
        && text.charAt(10) == 'T'
        && text.charAt(13) == ':'
        && text.charAt(16) == ':';
  }

  /** {@code YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)} as Unix milliseconds, or null. */
  private static Double parseInstant(String text) {
    if (!hasShape(text)) {
      return null;
    }
    Long days = parseDate(text);
    int seconds = parseTime(text, 11);
    if (days == null || seconds < 0) {
      return null;
    }
    int end = 19;
    int ms = 0;
    if (text.charAt(19) == '.') {
      end = 20;
      while (end < text.length() && Strings.isDigit(text.charAt(end))) {
        end++;
      }
      if (end == 20) {
        return null;
      }
      int kept = Math.min(end - 20, 3);
      int scale = kept == 1 ? 100 : kept == 2 ? 10 : 1;
      ms = digits(text, 20, kept) * scale;
    }
    Integer offset = parseOffset(text, end);
    if (offset == null) {
      return null;
    }
    return (double) (days * 86_400_000L + seconds * 1000L + ms - offset * 60_000L);
  }

  /** A finite number is Unix milliseconds, a string is parsed, anything else is not an instant. */
  static Double toInstant(Object value) {
    if (value instanceof Double d) {
      return Double.isFinite(d) ? d : null;
    }
    return value instanceof String s ? parseInstant(s) : null;
  }
}
