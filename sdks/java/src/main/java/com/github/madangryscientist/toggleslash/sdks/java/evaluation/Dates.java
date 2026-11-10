package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * Instants, to the "Dates" section of {@code docs/evaluation.md}. A hand-written walk over the
 * string: no {@code java.time} parser, so the accepted forms are exactly the specification's. The
 * result is Unix milliseconds as a binary64 number, or null when the text is not an instant.
 */
final class Dates {
  private Dates() {}

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  /** {@code length} ASCII digits at {@code start}, or -1 when any is not a digit. */
  private static int digits(String text, int start, int length) {
    int value = 0;
    for (int i = start; i < start + length; i++) {
      if (i >= text.length() || !isDigit(text.charAt(i))) {
        return -1;
      }
      value = value * 10 + (text.charAt(i) - '0');
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
    long y = month <= 2 ? year - 1L : year;
    long era = Math.floorDiv(y, 400L);
    long yearOfEra = y - era * 400;
    long monthIndex = month > 2 ? month - 3 : month + 9;
    long dayOfYear = (153 * monthIndex + 2) / 5 + day - 1;
    long dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear;
    return era * 146097 + dayOfEra - 719468;
  }

  /** The days since 1970-01-01 of {@code YYYY-MM-DD} at the start of the text, or null. */
  private static Long parseDate(String text) {
    int year = digits(text, 0, 4);
    int month = digits(text, 5, 2);
    int day = digits(text, 8, 2);
    if (year < 0 || month < 0 || day < 0) {
      return null;
    }
    if (year < 1 || month < 1 || month > 12) {
      return null;
    }
    if (day < 1 || day > daysInMonth(year, month)) {
      return null;
    }
    return daysFromCivil(year, month, day);
  }

  /** The seconds since midnight of {@code HH:MM:SS} at {@code start}, or -1. */
  private static int parseTime(String text, int start) {
    int hour = digits(text, start, 2);
    int minute = digits(text, start + 3, 2);
    int second = digits(text, start + 6, 2);
    if (hour < 0 || minute < 0 || second < 0) {
      return -1;
    }
    if (hour > 23 || minute > 59 || second > 59) {
      return -1;
    }
    return (hour * 60 + minute) * 60 + second;
  }

  /** The offset that is all of the text from {@code start}, in minutes east of UTC; null if not. */
  private static Integer parseOffset(String text, int start) {
    int length = text.length() - start;
    if (length == 1 && text.charAt(start) == 'Z') {
      return 0;
    }
    if (length != 6 || text.charAt(start + 3) != ':') {
      return null;
    }
    char signChar = text.charAt(start);
    if (signChar != '+' && signChar != '-') {
      return null;
    }
    int hour = digits(text, start + 1, 2);
    int minute = digits(text, start + 4, 2);
    if (hour < 0 || minute < 0 || hour > 23 || minute > 59) {
      return null;
    }
    return (signChar == '+' ? 1 : -1) * (hour * 60 + minute);
  }

  /** The separators in place; the shortest form is {@code 0001-01-01T00:00:00Z}. */
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
    int ms = 0;
    int end = 19;
    if (text.charAt(19) == '.') {
      end = 20;
      while (end < text.length() && isDigit(text.charAt(end))) {
        end++;
      }
      if (end == 20) {
        return null;
      }
      // The first three digits are the milliseconds; a shorter fraction is padded on the right.
      int kept = Math.min(end - 20, 3);
      int scale = kept == 1 ? 100 : kept == 2 ? 10 : 1;
      ms = digits(text, 20, kept) * scale;
    }
    Integer offsetMinutes = parseOffset(text, end);
    if (offsetMinutes == null) {
      return null;
    }
    long total = days * 86_400_000L + seconds * 1000L + ms - offsetMinutes * 60_000L;
    return (double) total;
  }

  /** A finite number is Unix milliseconds, a string is parsed, anything else is not an instant. */
  static Double toInstant(Object value) {
    if (value instanceof Double d) {
      return Double.isFinite(d) ? d : null;
    }
    return value instanceof String s ? parseInstant(s) : null;
  }
}
