namespace ToggleSlash.Evaluation;

/// <summary>
/// An instant written as <c>YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)</c>, to the "Dates" section of
/// docs/evaluation.md. Read by a hand-written walk over the characters: no regular expression, and none
/// of <c>DateTime.Parse</c>, <c>DateTimeOffset.Parse</c> or <c>Date.parse</c>, whose idea of what is valid
/// differs by runtime and culture. The result is Unix milliseconds as a binary64 number, which holds
/// every valid instant exactly.
/// </summary>
public static class RfcInstant
{
    /// <summary>The Unix milliseconds <paramref name="text"/> spells, or null when it is not an instant.</summary>
    public static double? Parse(string text)
    {
        // The shortest form is "0001-01-01T00:00:00Z".
        if (text.Length < 20) return null;
        if (text[4] != '-' || text[7] != '-' || text[10] != 'T' || text[13] != ':' || text[16] != ':') return null;
        if (Digits(text, 0, 4) is not { } year || Digits(text, 5, 2) is not { } month || Digits(text, 8, 2) is not { } day) return null;
        if (Digits(text, 11, 2) is not { } hour || Digits(text, 14, 2) is not { } minute || Digits(text, 17, 2) is not { } second) return null;
        if (year < 1 || month < 1 || month > 12) return null;
        if (day < 1 || day > DaysInMonth(year, month)) return null;
        if (hour > 23 || minute > 59 || second > 59) return null;

        var position = 19;
        long fraction = 0;
        if (text[position] == '.')
        {
            position++;
            var start = position;
            while (position < text.Length && IsDigit(text[position])) position++;
            if (position == start) return null;
            // The first three digits are the milliseconds; a shorter fraction is padded on the right.
            var kept = Math.Min(position - start, 3);
            fraction = Digits(text, start, kept)!.Value * (kept == 1 ? 100 : kept == 2 ? 10 : 1);
        }

        long offsetMinutes = 0;
        if (position < text.Length && text[position] == 'Z')
        {
            position++;
        }
        else
        {
            if (position >= text.Length || text[position] is not ('+' or '-')) return null;
            if (text.Length - position != 6 || text[position + 3] != ':') return null;
            if (Digits(text, position + 1, 2) is not { } offsetHour || Digits(text, position + 4, 2) is not { } offsetMinute) return null;
            if (offsetHour > 23 || offsetMinute > 59) return null;
            offsetMinutes = (text[position] == '+' ? 1 : -1) * (offsetHour * 60 + offsetMinute);
            position += 6;
        }
        if (position != text.Length) return null;

        var days = DaysFromCivil(year, month, day);
        return days * 86_400_000L + ((hour * 60 + minute) * 60 + second) * 1000 + fraction - offsetMinutes * 60_000;
    }

    private static bool IsDigit(char c) => c >= '0' && c <= '9';

    /// <summary><paramref name="length"/> ASCII digits at <paramref name="start"/>, as a number; null when any is not a digit.</summary>
    private static long? Digits(string text, int start, int length)
    {
        long value = 0;
        for (var i = start; i < start + length; i++)
        {
            if (!IsDigit(text[i])) return null;
            value = value * 10 + (text[i] - '0');
        }
        return value;
    }

    private static bool IsLeapYear(long year) => year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);

    private static long DaysInMonth(long year, long month) =>
        month == 2 ? (IsLeapYear(year) ? 29 : 28) : month is 4 or 6 or 9 or 11 ? 30 : 31;

    /// <summary>
    /// Days since 1970-01-01 in the proleptic Gregorian calendar ("days from civil"). For years 1 to 9999
    /// every value before the last subtraction is non-negative, so truncating division and a floor agree.
    /// </summary>
    private static long DaysFromCivil(long year, long month, long day)
    {
        var y = month <= 2 ? year - 1 : year;
        var era = y / 400;
        var yearOfEra = y - era * 400;
        var monthIndex = month > 2 ? month - 3 : month + 9;
        var dayOfYear = (153 * monthIndex + 2) / 5 + day - 1;
        var dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear;
        return era * 146097 + dayOfEra - 719468;
    }
}
