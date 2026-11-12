using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk.Evaluation;

/// <summary>The shape of a ruleset, checked at runtime because rulesets arrive as JSON. docs/evaluation.md is normative.</summary>
internal static class Validation
{
    /// <summary>A property of an object, a JSON null included; null when the property is absent.</summary>
    public static JsonValue? Get(JsonObject obj, string name) =>
        obj.TryGetValue(name, out var value) ? value : null;

    private static JsonValue? Get(JsonValue value, string name) => value is JsonObject obj ? Get(obj, name) : null;

    public static bool IsAbsent(JsonValue? value) => value is null or JsonNull;

    private static bool IsInteger(double number) => double.IsFinite(number) && number == Math.Floor(number);

    private static bool TryCount(JsonValue? value, out double count)
    {
        count = 0;
        if (value is not JsonNumber number)
        {
            return false;
        }

        count = number.Value;
        return IsInteger(count) && count >= 0;
    }

    private static bool TryIndex(JsonValue? value, int variationCount, out int index)
    {
        index = 0;
        if (!TryCount(value, out var count) || count >= variationCount)
        {
            return false;
        }

        index = (int)count;
        return true;
    }

    private static bool TryKey(JsonValue? value, out string key)
    {
        key = "";
        if (value is not JsonString { Value.Length: > 0 } text)
        {
            return false;
        }

        key = text.Value;
        return true;
    }

    private static bool TryString(JsonValue? value, out string text)
    {
        text = "";
        if (value is not JsonString str)
        {
            return false;
        }

        text = str.Value;
        return true;
    }

    private static bool TryNegate(JsonValue? value, out bool negate)
    {
        negate = false;
        if (IsAbsent(value))
        {
            return true;
        }

        if (value is not JsonBool flag)
        {
            return false;
        }

        negate = flag.Value;
        return true;
    }

    private static bool TryStrings(JsonValue? value, out List<string> strings)
    {
        strings = [];
        if (value is not JsonArray array)
        {
            return false;
        }

        foreach (var item in array.Items)
        {
            if (item is not JsonString text)
            {
                return false;
            }

            strings.Add(text.Value);
        }

        return true;
    }

    private static bool TryClauseValue(JsonValue value, out ClauseValue clauseValue)
    {
        clauseValue = default;
        switch (value)
        {
            case JsonString text:
                clauseValue = new ClauseValue(ValueKind.String, text.Value, 0, false);
                return true;
            case JsonBool flag:
                clauseValue = new ClauseValue(ValueKind.Boolean, "", 0, flag.Value);
                return true;
            case JsonNumber number when double.IsFinite(number.Value):
                clauseValue = new ClauseValue(ValueKind.Number, "", number.Value, false);
                return true;
            default:
                return false;
        }
    }

    private static Op? AttributeOp(string? op) => op switch
    {
        "in" => Op.In,
        "contains" => Op.Contains,
        "startsWith" => Op.StartsWith,
        "endsWith" => Op.EndsWith,
        "lt" => Op.Lt,
        "lte" => Op.Lte,
        "gt" => Op.Gt,
        "gte" => Op.Gte,
        "semverEq" => Op.SemverEq,
        "semverLt" => Op.SemverLt,
        "semverLte" => Op.SemverLte,
        "semverGt" => Op.SemverGt,
        "semverGte" => Op.SemverGte,
        "before" => Op.Before,
        "after" => Op.After,
        "matches" => Op.Matches,
        _ => null,
    };

    private static string? OpName(JsonObject value) => Get(value, "op") is JsonString op ? op.Value : null;

    /// <summary>An optional kind: absent is valid and null, anything but a valid kind is not.</summary>
    private static bool TryOptionalKind(JsonValue? value, out string? kind)
    {
        kind = null;
        if (IsAbsent(value))
        {
            return true;
        }

        if (!TryString(value, out var text) || !ContextKinds.IsKind(text))
        {
            return false;
        }

        kind = text;
        return true;
    }

    private static Clause? ReadAttributeClause(JsonValue raw)
    {
        if (raw is not JsonObject value || AttributeOp(OpName(value)) is not { } op)
        {
            return null;
        }

        if (!TryOptionalKind(Get(value, "contextKind"), out var contextKind) || !TryKey(Get(value, "attribute"), out var attribute))
        {
            return null;
        }

        if (attribute.StartsWith('/') && AttributeReference.Parse(attribute) is null)
        {
            return null;
        }

        if (!TryNegate(Get(value, "negate"), out var negate) || Get(value, "values") is not JsonArray values)
        {
            return null;
        }

        var parsed = new List<ClauseValue>();
        foreach (var item in values.Items)
        {
            if (!TryClauseValue(item, out var clauseValue))
            {
                return null;
            }

            parsed.Add(clauseValue);
        }

        return new Clause(op, attribute, parsed, [], negate, contextKind);
    }

    private static Clause? ReadSegmentClause(JsonValue raw)
    {
        if (raw is not JsonObject value || OpName(value) != "segmentMatch")
        {
            return null;
        }

        if (!TryNegate(Get(value, "negate"), out var negate) || !TryStrings(Get(value, "values"), out var keys))
        {
            return null;
        }

        return new Clause(Op.SegmentMatch, "", [], keys, negate);
    }

    private static List<T>? ReadAll<T>(JsonValue? value, Func<JsonValue, T?> read)
        where T : class
    {
        if (value is not JsonArray array)
        {
            return null;
        }

        var items = new List<T>();
        foreach (var item in array.Items)
        {
            if (read(item) is not { } parsed)
            {
                return null;
            }

            items.Add(parsed);
        }

        return items;
    }

    private static bool TryRollout(JsonValue? value, int variationCount, out List<WeightedVariation> rollout, out string? kind)
    {
        rollout = [];
        kind = null;
        if (value is not JsonObject obj || !TryOptionalKind(Get(obj, "contextKind"), out kind))
        {
            return false;
        }

        if (Get(obj, "variations") is not JsonArray { Count: > 0 } variations)
        {
            return false;
        }

        double total = 0;
        foreach (var item in variations.Items)
        {
            if (item is not JsonObject entry || !TryIndex(Get(entry, "variation"), variationCount, out var index))
            {
                return false;
            }

            if (!TryCount(Get(entry, "weight"), out var weight) || weight > Bucketing.BucketCount)
            {
                return false;
            }

            rollout.Add(new WeightedVariation(index, weight));
            total += weight;
        }

        return total == Bucketing.BucketCount;
    }

    /// <summary>Exactly one of <c>variation</c> or <c>rollout</c>.</summary>
    private static Serve? ReadServe(JsonValue raw, int variationCount)
    {
        if (raw is not JsonObject value)
        {
            return null;
        }

        var variation = Get(value, "variation");
        var rollout = Get(value, "rollout");
        if (IsAbsent(rollout))
        {
            return TryIndex(variation, variationCount, out var index) ? new Serve(index, null) : null;
        }

        return IsAbsent(variation) && TryRollout(rollout, variationCount, out var weighted, out var kind) ? new Serve(null, weighted, kind) : null;
    }

    private static Rule? ReadRule(JsonValue raw, int variationCount)
    {
        if (raw is not JsonObject value)
        {
            return null;
        }

        var clauses = ReadAll(Get(value, "clauses"), clause => ReadAttributeClause(clause) ?? ReadSegmentClause(clause));
        if (clauses is null || ReadServe(value, variationCount) is not { } serve)
        {
            return null;
        }

        return new Rule(clauses, serve);
    }

    /// <summary>What serving the off variation needs: key, on, variations and offVariation.</summary>
    public static FlagHeader? ReadHeader(JsonValue raw)
    {
        if (raw is not JsonObject value || Get(value, "on") is not JsonBool on || !TryKey(Get(value, "key"), out var key))
        {
            return null;
        }

        if (Get(value, "variations") is not JsonArray { Count: > 0 } variations)
        {
            return null;
        }

        var values = variations.Items.ToList();
        var offVariation = Get(value, "offVariation");
        int? off = null;
        if (!IsAbsent(offVariation))
        {
            if (!TryIndex(offVariation, values.Count, out var index))
            {
                return null;
            }

            off = index;
        }

        return new FlagHeader(key, on.Value, values, off);
    }

    /// <summary>The rest of the flag, or null when anything in it is invalid.</summary>
    public static Flag? ReadBody(JsonObject value, FlagHeader header)
    {
        var count = header.Variations.Count;
        if (!TryString(Get(value, "salt"), out var salt) || !TryCount(Get(value, "version"), out var version))
        {
            return null;
        }

        var targets = new Dictionary<string, Dictionary<string, TargetHit>>(StringComparer.Ordinal);
        var rawTargets = ReadAll(Get(value, "targets"), item => ReadTarget(item, count));
        if (rawTargets is null)
        {
            return null;
        }

        for (var order = 0; order < rawTargets.Count; order++)
        {
            var target = rawTargets[order];
            if (!targets.TryGetValue(target.Kind, out var keys))
            {
                targets[target.Kind] = keys = new Dictionary<string, TargetHit>(StringComparer.Ordinal);
            }

            foreach (var contextKey in target.ContextKeys)
            {
                keys.TryAdd(contextKey, new TargetHit(order, target.Variation));
            }
        }

        var prerequisites = ReadAll(Get(value, "prerequisites"), ReadPrerequisite);
        var rules = ReadAll(Get(value, "rules"), item => ReadRule(item, count));
        if (prerequisites is null || rules is null)
        {
            return null;
        }

        var fallthrough = Get(value, "fallthrough") is { } raw ? ReadServe(raw, count) : null;
        return fallthrough is null ? null : new Flag(header, salt, version, targets, prerequisites, rules, fallthrough);
    }

    private static Target? ReadTarget(JsonValue raw, int variationCount)
    {
        if (raw is not JsonObject value || !TryIndex(Get(value, "variation"), variationCount, out var variation))
        {
            return null;
        }

        if (!TryOptionalKind(Get(value, "contextKind"), out var kind))
        {
            return null;
        }

        return TryStrings(Get(value, "contextKeys"), out var keys) ? new Target(variation, keys, kind ?? ContextKinds.Default) : null;
    }

    private static Prerequisite? ReadPrerequisite(JsonValue raw)
    {
        if (raw is not JsonObject value || !TryKey(Get(value, "flagKey"), out var flagKey) || !TryCount(Get(value, "variation"), out var variation))
        {
            return null;
        }

        return new Prerequisite(flagKey, variation);
    }

    private static SegmentRule? ReadSegmentRule(JsonValue raw)
    {
        if (raw is not JsonObject value)
        {
            return null;
        }

        var weight = Get(value, "weight");
        double? parsedWeight = null;
        if (!IsAbsent(weight))
        {
            if (!TryCount(weight, out var count) || count > Bucketing.BucketCount)
            {
                return null;
            }

            parsedWeight = count;
        }

        if (!TryOptionalKind(Get(value, "rolloutContextKind"), out var rolloutKind))
        {
            return null;
        }

        var clauses = ReadAll(Get(value, "clauses"), ReadAttributeClause);
        return clauses is null ? null : new SegmentRule(clauses, parsedWeight, rolloutKind);
    }

    /// <summary>
    /// Keys by kind: <paramref name="userKeys"/> under <c>user</c>, merged with an optional array of
    /// <c>{ contextKind, values }</c>. Null when the array is present and malformed.
    /// </summary>
    private static Dictionary<string, HashSet<string>>? ReadKeysByKind(List<string> userKeys, JsonValue? lists)
    {
        var index = new Dictionary<string, HashSet<string>>(StringComparer.Ordinal)
        {
            [ContextKinds.Default] = userKeys.ToHashSet(StringComparer.Ordinal),
        };
        if (IsAbsent(lists))
        {
            return index;
        }

        if (lists is not JsonArray array)
        {
            return null;
        }

        foreach (var item in array.Items)
        {
            if (item is not JsonObject entry || !TryString(Get(entry, "contextKind"), out var kind) || !ContextKinds.IsKind(kind))
            {
                return null;
            }

            if (!TryStrings(Get(entry, "values"), out var values))
            {
                return null;
            }

            if (!index.TryGetValue(kind, out var keys))
            {
                index[kind] = keys = new HashSet<string>(StringComparer.Ordinal);
            }

            keys.UnionWith(values);
        }

        return index;
    }

    /// <summary>A segment, or null when it is malformed.</summary>
    public static Segment? ReadSegment(JsonValue raw)
    {
        if (raw is not JsonObject value)
        {
            return null;
        }

        if (!TryKey(Get(value, "key"), out var key) || !TryString(Get(value, "salt"), out var salt))
        {
            return null;
        }

        if (!TryStrings(Get(value, "included"), out var included) || !TryStrings(Get(value, "excluded"), out var excluded))
        {
            return null;
        }

        var includedByKind = ReadKeysByKind(included, Get(value, "includedContexts"));
        var excludedByKind = ReadKeysByKind(excluded, Get(value, "excludedContexts"));
        if (includedByKind is null || excludedByKind is null)
        {
            return null;
        }

        var rules = ReadAll(Get(value, "rules"), ReadSegmentRule);
        return rules is null ? null : new Segment(key, salt, includedByKind, excludedByKind, rules);
    }
}
