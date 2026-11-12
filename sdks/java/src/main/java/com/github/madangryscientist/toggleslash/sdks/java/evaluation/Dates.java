package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

/**
 * Instants, to the "Dates" section of the evaluation spec: a hand-written walk over the string,
 * with no library, so the result is Unix milliseconds as a double, or null when the text is not an
 * instant.
 */
final class Dates {
  private Dates() {}

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  /**
   * {@code length} ASCII digits at {@code start} as a number; -1 when any is missing or not a
   * digit.
   */
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

  /**
   * Days since 1970-01-01 in the proleptic Gregorian calendar ("days from civil"), years 1 to 9999.
   */
  private static long daysFromCivil(int year, int month, int day) {
    int y = month <= 2 ? year - 1 : year;
    int era = Math.floorDiv(y, 400);
    int yearOfEra = y - era * 400;
    int monthIndex = month > 2 ? month - 3 : month + 9;
    int dayOfYear = (153 * monthIndex + 2) / 5 + day - 1;
    int dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear;
    return (long) era * 146097 + dayOfEra - 719468;
  }

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

  private static int parseTime(String text, int at) {
    int hour = digits(text, at, 2);
    int minute = digits(text, at + 3, 2);
    int second = digits(text, at + 6, 2);
    if (hour < 0 || minute < 0 || second < 0) {
      return -1;
    }
    if (hour > 23 || minute > 59 || second > 59) {
      return -1;
    }
    return (hour * 60 + minute) * 60 + second;
  }

  private static boolean hasShape(String text) {
    return text.length() >= 20
        && text.charAt(4) == '-'
        && text.charAt(7) == '-'
        && text.charAt(10) == 'T'
        && text.charAt(13) == ':'
        && text.charAt(16) == ':';
  }

  /**
   * The offset ({@code Z}, plus or minus HH:MM) that is all of the text from {@code at}, in minutes
   * east of UTC.
   */
  private static Integer parseOffset(String text, int at) {
    int length = text.length() - at;
    if (length == 1 && text.charAt(at) == 'Z') {
      return 0;
    }
    if (length != 6) {
      return null;
    }
    char signChar = text.charAt(at);
    if ((signChar != '+' && signChar != '-') || text.charAt(at + 3) != ':') {
      return null;
    }
    int hour = digits(text, at + 1, 2);
    int minute = digits(text, at + 4, 2);
    if (hour < 0 || minute < 0 || hour > 23 || minute > 59) {
      return null;
    }
    return (signChar == '+' ? 1 : -1) * (hour * 60 + minute);
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
    int milliseconds = 0;
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
      milliseconds = digits(text, 20, kept) * scale;
    }
    Integer offsetMinutes = parseOffset(text, end);
    if (offsetMinutes == null) {
      return null;
    }
    return (double) (days * 86_400_000L + seconds * 1000L + milliseconds - offsetMinutes * 60_000L);
  }

  /** A finite number is Unix milliseconds, a string is parsed, anything else is not an instant. */
  static Double toInstant(Object value) {
    if (value instanceof Double) {
      double d = (Double) value;
      return Double.isNaN(d) || Double.isInfinite(d) ? null : (Double) value;
    }
    return value instanceof String ? parseInstant((String) value) : null;
  }
}
