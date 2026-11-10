using System.Text.Json;

namespace ToggleSlash.Sdk.Evaluation;

/// <summary>Attribute clauses, to the "Clauses" section of docs/evaluation.md. There is no coercion anywhere.</summary>
internal static class ClauseMatcher
{
    /// <summary>
    /// The value a name or a <c>/</c> reference reaches, or null when it is missing or null. Only the context's
    /// own <c>attributes</c> count, <c>key</c> always reads the context key, and a step goes into an object only.
    /// </summary>
    private static JsonElement? ReadAttribute(KindContext context, string name)
    {
        // Load refused an invalid reference, so Parse does not return null here.
        var components = name.StartsWith('/') ? AttributeReference.Parse(name) ?? [] : [name];
        if (components.Count == 0) return null;
        JsonElement current;
        if (components[0] == "key") current = context.KeyElement;
        else if (context.Attributes is not { } attributes || !attributes.TryGetProperty(components[0], out current)) return null;
        for (var i = 1; i < components.Count; i++)
        {
            if (current.ValueKind != JsonValueKind.Object || !current.TryGetProperty(components[i], out current)) return null;
        }
        return current.ValueKind == JsonValueKind.Null ? null : current;
    }

    /// <summary>
    /// Does one attribute clause hold? It reads the context of its <c>contextKind</c>. A missing attribute, or a
    /// missing kind, fails it, whatever <c>negate</c> says.
    /// </summary>
    public static bool AttributeClauseMatches(Clause clause, IReadOnlyDictionary<string, KindContext> contexts)
    {
        if (!contexts.TryGetValue(clause.ContextKind ?? ContextKinds.Default, out var context)) return false;
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
            case Op.Matches:
                // Compiled per call; an invalid pattern does not match.
                if (attribute.ValueKind != JsonValueKind.String || value.Kind != ValueKind.String) return false;
                return Pattern.Compile(value.String) is { } pattern && pattern.IsMatch(JsonStrings.Read(attribute));
            case Op.Before or Op.After:
                if (ToInstant(attribute) is not { } instant || ToInstant(value) is not { } limit) return false;
                return op == Op.Before ? instant < limit : instant > limit;
            default:
                return Compare(attribute, value) is { } order && MatchesOrder(op, order);
        }
    }

    /// <summary>A finite number is Unix milliseconds, a string is parsed, anything else is not an instant.</summary>
    private static double? ToInstant(JsonElement attribute) => attribute.ValueKind switch
    {
        JsonValueKind.Number => Validation.TryNumber(attribute, out var number) && double.IsFinite(number) ? number : null,
        JsonValueKind.String => RfcInstant.Parse(JsonStrings.Read(attribute)),
        _ => null,
    };

    private static double? ToInstant(ClauseValue value) => value.Kind switch
    {
        ValueKind.Number => double.IsFinite(value.Number) ? value.Number : null,
        ValueKind.String => RfcInstant.Parse(value.String),
        _ => null,
    };

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
