using System.Text.Json;
using System.Text.RegularExpressions;

namespace FeatureSlash.Server.Catalogue;

/// <summary>The rules the catalogue API enforces on what a client sends. A failure is a message for a 400.</summary>
public static partial class Validation
{
    public const int MaxNameLength = 200;
    public const int MaxDescriptionLength = 2000;
    public const int MaxOwnerLength = 200;
    public const int MaxTags = 20;
    public const int MaxVariations = 100;

    /// <summary>Project, environment, flag and tag keys: lowercase, digits, <c>_</c> and <c>-</c>, up to 64, no leading separator.</summary>
    [GeneratedRegex("^[a-z0-9][a-z0-9_-]{0,63}$")]
    private static partial Regex KeyPattern();

    public static string? Key(string? value, string field) =>
        value is not null && KeyPattern().IsMatch(value)
            ? null
            : $"{field} must match ^[a-z0-9][a-z0-9_-]{{0,63}}$.";

    public static string? Name(string? value, string field = "name") =>
        string.IsNullOrWhiteSpace(value) ? $"{field} is required."
        : value.Trim().Length > MaxNameLength ? $"{field} must be at most {MaxNameLength} characters."
        : null;

    /// <summary>At least 2, at most <see cref="MaxVariations"/>, none <c>null</c>, all one JSON type.</summary>
    public static string? Variations(IReadOnlyList<JsonElement>? variations)
    {
        if (variations is null)
        {
            return "variations is required.";
        }
        if (variations.Count < 2)
        {
            return "variations needs at least 2 values.";
        }
        if (variations.Count > MaxVariations)
        {
            return $"variations takes at most {MaxVariations} values.";
        }
        if (variations.Any(v => v.ValueKind is JsonValueKind.Null or JsonValueKind.Undefined))
        {
            return "a variation may not be null.";
        }
        return variations.Select(JsonType).Distinct().Count() > 1
            ? "variations must all be one JSON type (boolean, number, string, array or object)."
            : null;
    }

    /// <summary>Null when every tag is a valid key; the caller dedupes with <see cref="DistinctTags"/>.</summary>
    public static string? Tags(IReadOnlyList<string>? tags)
    {
        if (tags is null)
        {
            return null;
        }
        if (tags.Any(t => t is null || Key(t, "tag") is not null))
        {
            return "every tag must match ^[a-z0-9][a-z0-9_-]{0,63}$.";
        }
        return tags.Distinct().Count() > MaxTags ? $"a flag takes at most {MaxTags} tags." : null;
    }

    public static List<string> DistinctTags(IEnumerable<string> tags) => tags.Distinct().ToList();

    private static string JsonType(JsonElement value) =>
        value.ValueKind is JsonValueKind.True or JsonValueKind.False ? "boolean" : value.ValueKind.ToString();
}
