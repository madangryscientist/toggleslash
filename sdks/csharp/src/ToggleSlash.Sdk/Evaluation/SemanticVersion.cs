namespace ToggleSlash.Sdk.Evaluation;

/// <summary>
/// A semantic version, to the "Semantic versions" section of docs/evaluation.md. Parsed by a hand-written
/// walk over the characters, with no regular expression: <c>\d</c> matches Unicode digits in .NET, and
/// the TypeScript engine reads the same way. Numeric parts stay strings and are compared by length and
/// then by code unit, so they have no size limit and are never converted to a number.
/// </summary>
internal sealed class SemanticVersion
{
    private SemanticVersion(string major, string minor, string patch, string[] prerelease)
    {
        Major = major;
        Minor = minor;
        Patch = patch;
        Prerelease = prerelease;
    }

    private string Major { get; }

    private string Minor { get; }

    private string Patch { get; }

    /// <summary>The dot-separated prerelease identifiers; empty when there is no prerelease.</summary>
    private string[] Prerelease { get; }

    /// <summary>The version <paramref name="text"/> spells, or null when it is not one.</summary>
    public static SemanticVersion? Parse(string text)
    {
        var core = text;
        string[] prerelease = [];
        var plus = core.IndexOf('+', StringComparison.Ordinal);
        if (plus >= 0)
        {
            // Build metadata: identifiers only, and leading zeros are fine.
            if (Identifiers(core[(plus + 1)..]) is null) return null;
            core = core[..plus];
        }
        var dash = core.IndexOf('-', StringComparison.Ordinal);
        if (dash >= 0)
        {
            if (Identifiers(core[(dash + 1)..]) is not { } parts) return null;
            foreach (var part in parts)
            {
                if (AllDigits(part) && !IsNumericPart(part)) return null;
            }
            prerelease = parts;
            core = core[..dash];
        }
        var numbers = core.Split('.');
        // The short forms (`2`, `2.1`) carry no suffix.
        if (numbers.Length < 3 && (plus >= 0 || dash >= 0)) return null;
        if (numbers.Length > 3 || !numbers.All(IsNumericPart)) return null;
        return new SemanticVersion(numbers[0], numbers.Length > 1 ? numbers[1] : "0", numbers.Length > 2 ? numbers[2] : "0", prerelease);
    }

    /// <summary>-1, 0 or 1 by SemVer 2.0.0 section 11. Build metadata is already gone.</summary>
    public int CompareTo(SemanticVersion other)
    {
        var core = CompareNumeric(Major, other.Major);
        if (core == 0) core = CompareNumeric(Minor, other.Minor);
        if (core == 0) core = CompareNumeric(Patch, other.Patch);
        if (core != 0) return core;
        if (Prerelease.Length == 0 || other.Prerelease.Length == 0)
        {
            return Prerelease.Length == other.Prerelease.Length ? 0 : Prerelease.Length == 0 ? 1 : -1;
        }
        var shared = Math.Min(Prerelease.Length, other.Prerelease.Length);
        for (var i = 0; i < shared; i++)
        {
            var order = CompareIdentifier(Prerelease[i], other.Prerelease[i]);
            if (order != 0) return order;
        }
        return Math.Sign(Prerelease.Length.CompareTo(other.Prerelease.Length));
    }

    private static bool IsDigit(char c) => c is >= '0' and <= '9';

    private static bool IsIdentifierChar(char c) => c is (>= '0' and <= '9') or (>= 'A' and <= 'Z') or (>= 'a' and <= 'z') or '-';

    private static bool AllDigits(string text) => text.All(IsDigit);

    /// <summary><c>0</c> or digits with no leading zero.</summary>
    private static bool IsNumericPart(string text) => text.Length > 0 && AllDigits(text) && (text == "0" || text[0] != '0');

    /// <summary>Dot-separated, none empty, each of ASCII letters, digits and <c>-</c>.</summary>
    private static string[]? Identifiers(string text)
    {
        var parts = text.Split('.');
        return parts.All(part => part.Length > 0 && part.All(IsIdentifierChar)) ? parts : null;
    }

    /// <summary>Digit strings with no leading zero: the longer is larger, else by code unit.</summary>
    private static int CompareNumeric(string a, string b) =>
        a.Length != b.Length ? (a.Length < b.Length ? -1 : 1) : Math.Sign(string.CompareOrdinal(a, b));

    private static int CompareIdentifier(string a, string b)
    {
        var aNumeric = AllDigits(a);
        var bNumeric = AllDigits(b);
        if (aNumeric && bNumeric) return CompareNumeric(a, b);
        if (aNumeric) return -1;
        if (bNumeric) return 1;
        return Math.Sign(string.CompareOrdinal(a, b));
    }
}
