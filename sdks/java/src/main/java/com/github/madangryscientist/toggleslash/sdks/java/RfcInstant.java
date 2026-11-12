package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * An instant written as {@code YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)}, read by a hand-written
 * walk over the characters (no regular expression and no {@code java.time} parser, whose idea of
 * what is valid differs). The result is Unix milliseconds as a binary64 number, which holds every
 * valid instant.
 */
final class RfcInstant {
  private RfcInstant() {}

  /**
   * Parses an instant.
   *
   * @param text the candidate
   * @return Unix milliseconds, or null when {@code text} is not an instant
   */
  static Double parse(String text) {
    if (text.length() < 20) {
      return null;
    }
    if (text.charAt(4) != '-'
        || text.charAt(7) != '-'
        || text.charAt(10) != 'T'
        || text.charAt(13) != ':'
        || text.charAt(16) != ':') {
      return null;
    }
    long year = digits(text, 0, 4);
    long month = digits(text, 5, 2);
    long day = digits(text, 8, 2);
    long hour = digits(text, 11, 2);
    long minute = digits(text, 14, 2);
    long second = digits(text, 17, 2);
    if (year < 0 || month < 0 || day < 0 || hour < 0 || minute < 0 || second < 0) {
      return null;
    }
    if (year < 1 || month < 1 || month > 12) {
      return null;
    }
    if (day < 1 || day > daysInMonth(year, month)) {
      return null;
    }
    if (hour > 23 || minute > 59 || second > 59) {
      return null;
    }
    int position = 19;
    long fraction = 0;
    if (text.charAt(position) == '.') {
      position++;
      int start = position;
      while (position < text.length() && isDigit(text.charAt(position))) {
        position++;
      }
      if (position == start) {
        return null;
      }
      int kept = Math.min(position - start, 3);
      fraction = digits(text, start, kept) * (kept == 1 ? 100 : kept == 2 ? 10 : 1);
    }
    long offsetMinutes = 0;
    if (position < text.length() && text.charAt(position) == 'Z') {
      position++;
    } else {
      if (position >= text.length()
          || (text.charAt(position) != '+' && text.charAt(position) != '-')) {
        return null;
      }
      if (text.length() - position != 6 || text.charAt(position + 3) != ':') {
        return null;
      }
      long offsetHour = digits(text, position + 1, 2);
      long offsetMinute = digits(text, position + 4, 2);
      if (offsetHour < 0 || offsetMinute < 0 || offsetHour > 23 || offsetMinute > 59) {
        return null;
      }
      offsetMinutes = (text.charAt(position) == '+' ? 1 : -1) * (offsetHour * 60 + offsetMinute);
      position += 6;
    }
    if (position != text.length()) {
      return null;
    }
    long days = daysFromCivil(year, month, day);
    return (double)
        (days * 86_400_000L
            + ((hour * 60 + minute) * 60 + second) * 1000
            + fraction
            - offsetMinutes * 60_000);
  }

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  /** {@code length} ASCII digits at {@code start} as a number, or -1 when any is not a digit. */
  private static long digits(String text, int start, int length) {
    long value = 0;
    for (int i = start; i < start + length; i++) {
      if (!isDigit(text.charAt(i))) {
        return -1;
      }
      value = value * 10 + (text.charAt(i) - '0');
    }
    return value;
  }

  private static boolean isLeapYear(long year) {
    return year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
  }

  private static long daysInMonth(long year, long month) {
    if (month == 2) {
      return isLeapYear(year) ? 29 : 28;
    }
    return month == 4 || month == 6 || month == 9 || month == 11 ? 30 : 31;
  }

  /** Days since 1970-01-01 in the proleptic Gregorian calendar. */
  private static long daysFromCivil(long year, long month, long day) {
    long y = month <= 2 ? year - 1 : year;
    long era = y / 400;
    long yearOfEra = y - era * 400;
    long monthIndex = month > 2 ? month - 3 : month + 9;
    long dayOfYear = (153 * monthIndex + 2) / 5 + day - 1;
    long dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear;
    return era * 146097 + dayOfEra - 719468;
  }
}
