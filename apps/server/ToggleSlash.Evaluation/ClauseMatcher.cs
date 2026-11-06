using System.Text.Json;

namespace ToggleSlash.Evaluation;

/// <summary>Attribute clauses, to the "Clauses" section of docs/evaluation.md. There is no coercion anywhere.</summary>
internal static class ClauseMatcher
{
    /// <summary>
    /// The attribute named <paramref name="name"/>, or null when it is missing or null. Only the context's
    /// own <c>attributes</c> count, and <c>key</c> always reads the context key.
    /// </summary>
    private static JsonElement? ReadAttribute(JsonElement context, string name)
    {
        if (name == "key") return context.GetProperty("key");
        if (!context.TryGetProperty("attributes", out var attributes) || attributes.ValueKind != JsonValueKind.Object) return null;
        if (!attributes.TryGetProperty(name, out var value) || value.ValueKind == JsonValueKind.Null) return null;
        return value;
    }

    /// <summary>Does one attribute clause hold? A missing attribute fails it, whatever <c>negate</c> says.</summary>
    public static bool AttributeClauseMatches(Clause clause, JsonElement context)
    {
        if (ReadAttribute(context, clause.Attribute) is not { } attribute) return false;
        var matched = attribute.ValueKind == JsonValueKind.Array
            ? attribute.EnumerateArray().Any(candidate => AnyValueMatches(clause, candidate))
            : AnyValueMatches(clause, attribute);
        return clause.Negate ? !matched : matched;
    }

    private static bool AnyValueMatches(Clause clause, JsonElement candidate)
    {
        foreach (var value in clause.Values)
        {
            if (Matches(clause.Op, candidate, value)) return true;
        }
        return false;
    }

    private static bool Matches(Op op, JsonElement attribute, ClauseValue value)
    {
        switch (op)
        {
            case Op.In:
                return attribute.ValueKind switch
                {
                    JsonValueKind.String => value.Kind == ValueKind.String
                        && string.Equals(JsonStrings.Read(attribute), value.String, StringComparison.Ordinal),
                    JsonValueKind.True or JsonValueKind.False => value.Kind == ValueKind.Boolean
                        && (attribute.ValueKind == JsonValueKind.True) == value.Boolean,
                    JsonValueKind.Number => value.Kind == ValueKind.Number
                        && Validation.TryNumber(attribute, out var number) && double.IsFinite(number) && number == value.Number,
                    _ => false,
                };
            case Op.Contains or Op.StartsWith or Op.EndsWith:
                if (attribute.ValueKind != JsonValueKind.String || value.Kind != ValueKind.String) return false;
                var text = JsonStrings.Read(attribute);
                // Ordinal every time: the culture-sensitive defaults are exactly what the vectors catch.
                return op switch
                {
                    Op.Contains => text.Contains(value.String, StringComparison.Ordinal),
                    Op.StartsWith => text.StartsWith(value.String, StringComparison.Ordinal),
                    _ => text.EndsWith(value.String, StringComparison.Ordinal),
                };
            case Op.SemverEq or Op.SemverLt or Op.SemverLte or Op.SemverGt or Op.SemverGte:
                if (attribute.ValueKind != JsonValueKind.String || value.Kind != ValueKind.String) return false;
                if (SemanticVersion.Parse(JsonStrings.Read(attribute)) is not { } candidate || SemanticVersion.Parse(value.String) is not { } target) return false;
                var versionOrder = candidate.CompareTo(target);
                return op == Op.SemverEq ? versionOrder == 0 : MatchesOrder(op, versionOrder);
            default:
                return Compare(attribute, value) is { } order && MatchesOrder(op, order);
        }
    }

    /// <summary>-1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise null.</summary>
    private static int? Compare(JsonElement attribute, ClauseValue value)
    {
        if (attribute.ValueKind == JsonValueKind.Number && value.Kind == ValueKind.Number)
        {
            if (!Validation.TryNumber(attribute, out var number) || !double.IsFinite(number)) return null;
            return number < value.Number ? -1 : number > value.Number ? 1 : 0;
        }
        if (attribute.ValueKind == JsonValueKind.String && value.Kind == ValueKind.String)
        {
            return Math.Sign(string.CompareOrdinal(JsonStrings.Read(attribute), value.String));
        }
        return null;
    }

    private static bool MatchesOrder(Op op, int order) => op switch
    {
        Op.Lt or Op.SemverLt => order < 0,
        Op.Lte or Op.SemverLte => order <= 0,
        Op.Gt or Op.SemverGt => order > 0,
        _ => order >= 0,
    };
}
