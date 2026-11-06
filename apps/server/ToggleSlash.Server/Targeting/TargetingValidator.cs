using System.Text.Json;

namespace ToggleSlash.Server.Targeting;

/// <summary>
/// The shape rules of docs/evaluation.md for one flag's per-environment fields and for a segment, checked
/// on what a client sends. Stricter than the spec where an API should be: an unknown field is a
/// mistake, not something to ignore. A problem is a message for a 400 that names the field. What a
/// reference points at (a flag, a segment) is the endpoint's to check, so this collects the references.
/// </summary>
internal sealed class TargetingValidator(int variationCount)
{
    public const int MaxWeight = 100_000;

    private static readonly string[] AttributeOps = ["in", "contains", "startsWith", "endsWith", "lt", "lte", "gt", "gte"];

    /// <summary>Every prerequisite, in order, for the endpoint to resolve and to check for cycles.</summary>
    public List<PrerequisiteReference> Prerequisites { get; } = [];

    /// <summary>Every segment key a rule refers to, with where, for the endpoint to resolve.</summary>
    public List<SegmentReference> Segments { get; } = [];

    public string? Targets(JsonElement? value)
    {
        var (problem, items) = Items(value, "targets");
        if (problem is not null)
        {
            return problem;
        }
        for (var i = 0; i < items.Count; i++)
        {
            var path = $"targets[{i}]";
            if (Fields(items[i], path, "variation", "contextKeys") is { } fields)
            {
                return fields;
            }
            if (Index(items[i], "variation", path) is { } index)
            {
                return index;
            }
            if (Strings(items[i], "contextKeys", path, required: true) is { } keys)
            {
                return keys;
            }
        }
        return null;
    }

    public string? Prerequisite(JsonElement? value)
    {
        var (problem, items) = Items(value, "prerequisites");
        if (problem is not null)
        {
            return problem;
        }
        for (var i = 0; i < items.Count; i++)
        {
            var path = $"prerequisites[{i}]";
            if (Fields(items[i], path, "flagKey", "variation") is { } fields)
            {
                return fields;
            }
            if (!items[i].TryGetProperty("flagKey", out var flagKey) || flagKey.ValueKind != JsonValueKind.String || flagKey.GetString() is not { Length: > 0 } key)
            {
                return $"{path}.flagKey must be a flag key.";
            }
            if (!items[i].TryGetProperty("variation", out var variation) || !TryInteger(variation, out var index) || index < 0)
            {
                return $"{path}.variation must be a whole number, 0 or more.";
            }
            Prerequisites.Add(new PrerequisiteReference(key, index, path));
        }
        return null;
    }

    public string? Rules(JsonElement? value)
    {
        var (problem, items) = Items(value, "rules");
        if (problem is not null)
        {
            return problem;
        }
        for (var i = 0; i < items.Count; i++)
        {
            var path = $"rules[{i}]";
            var rule = items[i];
            if (Fields(rule, path, "clauses", "variation", "rollout") is { } fields)
            {
                return fields;
            }
            var (clauseProblem, clauseItems) = Items(rule.TryGetProperty("clauses", out var clauses) ? clauses : null, $"{path}.clauses", required: true);
            if (clauseProblem is not null)
            {
                return clauseProblem;
            }
            for (var c = 0; c < clauseItems.Count; c++)
            {
                if (Clause(clauseItems[c], $"{path}.clauses[{c}]", allowSegmentMatch: true) is { } clauseFault)
                {
                    return clauseFault;
                }
            }
            if (Serve(rule, path) is { } serve)
            {
                return serve;
            }
        }
        return null;
    }

    public string? Fallthrough(JsonElement? value) =>
        value is { ValueKind: JsonValueKind.Object } serve
            ? Fields(serve, "fallthrough", "variation", "rollout") ?? Serve(serve, "fallthrough")
            : "fallthrough is required and must be an object.";

    /// <summary>
    /// Runs <paramref name="validate"/>, turning a string that cannot be read into a problem. A JSON
    /// string with a lone surrogate (<c>"\ud800"</c>) is valid JSON, but <c>JsonElement.GetString()</c> and
    /// <c>.Name</c> throw on it, and a client's input must never be a 500. Nothing in a saved config
    /// needs one, so it is refused.
    /// </summary>
    public static string? Guard(Func<string?> validate)
    {
        try
        {
            return validate();
        }
        catch (InvalidOperationException)
        {
            return "A string contains a lone UTF-16 surrogate, which is not valid Unicode.";
        }
    }

    /// <summary>Segment rules: attribute clauses only, so segments do not nest, and an optional weight.</summary>
    public string? SegmentRules(JsonElement? value)
    {
        var (problem, items) = Items(value, "rules");
        if (problem is not null)
        {
            return problem;
        }
        for (var i = 0; i < items.Count; i++)
        {
            var path = $"rules[{i}]";
            var rule = items[i];
            if (Fields(rule, path, "clauses", "weight") is { } fields)
            {
                return fields;
            }
            if (rule.TryGetProperty("weight", out var weight) && weight.ValueKind != JsonValueKind.Null
                && (!TryInteger(weight, out var count) || count < 0 || count > MaxWeight))
            {
                return $"{path}.weight must be a whole number from 0 to {MaxWeight}.";
            }
            var (clauseProblem, clauseItems) = Items(rule.TryGetProperty("clauses", out var clauses) ? clauses : null, $"{path}.clauses", required: true);
            if (clauseProblem is not null)
            {
                return clauseProblem;
            }
            for (var c = 0; c < clauseItems.Count; c++)
            {
                if (Clause(clauseItems[c], $"{path}.clauses[{c}]", allowSegmentMatch: false) is { } clauseFault)
                {
                    return clauseFault;
                }
            }
        }
        return null;
    }

    /// <summary>Strings that are context keys: an array of non-empty strings.</summary>
    public static string? ContextKeys(IReadOnlyList<string>? keys, string field) =>
        keys is not null && keys.Any(k => string.IsNullOrEmpty(k)) ? $"{field} must hold non-empty strings." : null;

    private string? Clause(JsonElement clause, string path, bool allowSegmentMatch)
    {
        if (clause.ValueKind != JsonValueKind.Object)
        {
            return $"{path} must be an object.";
        }
        if (!clause.TryGetProperty("op", out var op) || op.ValueKind != JsonValueKind.String)
        {
            return $"{path}.op is required.";
        }
        var name = op.GetString()!;
        if (name == "segmentMatch")
        {
            if (!allowSegmentMatch)
            {
                return $"{path}.op: a segment rule cannot use segmentMatch, so segments do not nest.";
            }
            // The spec ignores an attribute here; allowing it keeps a clause that evaluates the same one that saves.
            if (Fields(clause, path, "op", "values", "negate", "attribute") is { } fields)
            {
                return fields;
            }
            if (Negate(clause, path) is { } negate)
            {
                return negate;
            }
            if (Strings(clause, "values", path, required: true) is { } values)
            {
                return values;
            }
            foreach (var key in clause.GetProperty("values").EnumerateArray())
            {
                Segments.Add(new SegmentReference(key.GetString()!, $"{path}.values"));
            }
            return null;
        }
        if (!AttributeOps.Contains(name))
        {
            return $"{path}.op must be one of {string.Join(", ", AttributeOps)}{(allowSegmentMatch ? ", segmentMatch" : "")}.";
        }
        if (Fields(clause, path, "op", "attribute", "values", "negate") is { } attributeFields)
        {
            return attributeFields;
        }
        if (Negate(clause, path) is { } attributeNegate)
        {
            return attributeNegate;
        }
        if (!clause.TryGetProperty("attribute", out var attribute) || attribute.ValueKind != JsonValueKind.String || attribute.GetString() is not { Length: > 0 })
        {
            return $"{path}.attribute must be a non-empty string.";
        }
        if (!clause.TryGetProperty("values", out var valuesElement) || valuesElement.ValueKind != JsonValueKind.Array)
        {
            return $"{path}.values must be an array.";
        }
        var index = 0;
        foreach (var value in valuesElement.EnumerateArray())
        {
            var ok = value.ValueKind is JsonValueKind.String or JsonValueKind.True or JsonValueKind.False
                || value.ValueKind == JsonValueKind.Number && TryFinite(value);
            if (!ok)
            {
                return $"{path}.values[{index}] must be a string, a finite number or a boolean.";
            }
            index++;
        }
        return null;
    }

    private static string? Negate(JsonElement clause, string path) =>
        clause.TryGetProperty("negate", out var negate) && negate.ValueKind is not (JsonValueKind.True or JsonValueKind.False or JsonValueKind.Null)
            ? $"{path}.negate must be a boolean."
            : null;

    /// <summary>Exactly one of <c>variation</c> or <c>rollout</c>, on <paramref name="holder"/> (a rule or the fallthrough).</summary>
    private string? Serve(JsonElement holder, string path)
    {
        if (holder.ValueKind != JsonValueKind.Object)
        {
            return $"{path} must be an object.";
        }
        var hasVariation = holder.TryGetProperty("variation", out var variation) && variation.ValueKind != JsonValueKind.Null;
        var hasRollout = holder.TryGetProperty("rollout", out var rollout) && rollout.ValueKind != JsonValueKind.Null;
        if (hasVariation == hasRollout)
        {
            return $"{path} needs exactly one of variation or rollout.";
        }
        if (hasVariation)
        {
            return Index(holder, "variation", path);
        }
        if (rollout.ValueKind != JsonValueKind.Object)
        {
            return $"{path}.rollout must be an object.";
        }
        if (Fields(rollout, $"{path}.rollout", "variations") is { } fields)
        {
            return fields;
        }
        if (!rollout.TryGetProperty("variations", out var entries) || entries.ValueKind != JsonValueKind.Array || entries.GetArrayLength() == 0)
        {
            return $"{path}.rollout.variations must be an array with at least one entry.";
        }
        long total = 0;
        var i = 0;
        foreach (var entry in entries.EnumerateArray())
        {
            var entryPath = $"{path}.rollout.variations[{i}]";
            if (Fields(entry, entryPath, "variation", "weight") is { } entryFields)
            {
                return entryFields;
            }
            if (Index(entry, "variation", entryPath) is { } index)
            {
                return index;
            }
            if (!entry.TryGetProperty("weight", out var weight) || !TryInteger(weight, out var count) || count < 0 || count > MaxWeight)
            {
                return $"{entryPath}.weight must be a whole number from 0 to {MaxWeight}.";
            }
            total += count;
            i++;
        }
        return total == MaxWeight
            ? null
            : $"{path}.rollout weights must sum to {MaxWeight}, they sum to {total}.";
    }

    /// <summary>The array at <paramref name="value"/>; absent counts as empty unless <paramref name="required"/>.</summary>
    private static (string? Problem, List<JsonElement> Items) Items(JsonElement? value, string path, bool required = false)
    {
        if (value is null or { ValueKind: JsonValueKind.Null or JsonValueKind.Undefined })
        {
            return (required ? $"{path} is required (it may be empty)." : null, []);
        }
        return value.Value.ValueKind == JsonValueKind.Array
            ? (null, value.Value.EnumerateArray().ToList())
            : ($"{path} must be an array.", []);
    }

    private static string? Fields(JsonElement item, string path, params string[] allowed)
    {
        if (item.ValueKind != JsonValueKind.Object)
        {
            return $"{path} must be an object.";
        }
        var unknown = item.EnumerateObject().Select(p => p.Name).FirstOrDefault(name => !allowed.Contains(name));
        return unknown is null ? null : $"{path} has an unknown field \"{unknown}\" (allowed: {string.Join(", ", allowed)}).";
    }

    /// <summary>Variation index, in range of the flag's variations.</summary>
    private string? Index(JsonElement item, string name, string path)
    {
        if (!item.TryGetProperty(name, out var value) || !TryInteger(value, out var index) || index < 0 || index >= variationCount)
        {
            return $"{path}.{name} must be an index into the flag's variations (0 to {variationCount - 1}).";
        }
        return null;
    }

    private static string? Strings(JsonElement item, string name, string path, bool required)
    {
        if (!item.TryGetProperty(name, out var value) || value.ValueKind == JsonValueKind.Null)
        {
            return required ? $"{path}.{name} must be an array of non-empty strings." : null;
        }
        return value.ValueKind == JsonValueKind.Array && value.EnumerateArray().All(v => v.ValueKind == JsonValueKind.String && v.GetString() is { Length: > 0 })
            ? null
            : $"{path}.{name} must be an array of non-empty strings.";
    }

    /// <summary>A JSON number with no fractional part; <c>1.0</c> counts, as in the spec.</summary>
    private static bool TryInteger(JsonElement value, out long integer)
    {
        integer = 0;
        if (value.ValueKind != JsonValueKind.Number || !value.TryGetDouble(out var number) || !double.IsFinite(number)
            || number != Math.Floor(number) || Math.Abs(number) > 1e15)
        {
            return false;
        }
        integer = (long)number;
        return true;
    }

    private static bool TryFinite(JsonElement value) => value.TryGetDouble(out var number) && double.IsFinite(number);
}

internal sealed record PrerequisiteReference(string FlagKey, long Variation, string Path);

internal sealed record SegmentReference(string Key, string Path);
