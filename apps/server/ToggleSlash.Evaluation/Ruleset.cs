using System.Globalization;
using System.Text.Json;

namespace ToggleSlash.Evaluation;

/// <summary>
/// One environment's flags and segments, validated and indexed once so each evaluation skips that work.
/// <see cref="Load(JsonElement)"/> never throws: a bad flag becomes an <c>ERROR</c> result when it is
/// evaluated. The ruleset copies what it needs, so it does not depend on the document it came from.
/// </summary>
public sealed class Ruleset
{
    private readonly Dictionary<string, FlagEntry> _flags;
    // A null segment is a malformed one.
    private readonly Dictionary<string, Segment?> _segments;

    private Ruleset(Dictionary<string, FlagEntry> flags, Dictionary<string, Segment?> segments)
    {
        _flags = flags;
        _segments = segments;
    }

    internal bool TryGetFlag(string key, out FlagEntry entry) => _flags.TryGetValue(key, out entry!);

    internal bool TryGetSegment(string key, out Segment? segment) => _segments.TryGetValue(key, out segment);

    /// <summary>Loads a ruleset from its JSON form. A root that is not an object is an empty ruleset.</summary>
    public static Ruleset Load(JsonElement root)
    {
        JsonElement? rawFlags = null, rawSegments = null;
        if (root.ValueKind == JsonValueKind.Object)
        {
            rawFlags = Property(root, "flags");
            rawSegments = Property(root, "segments");
        }
        var segments = IndexByKey(rawSegments, item => Validation.ReadSegment(item));
        var flags = IndexByKey(rawFlags, item => CompileFlag(item, segments));
        return new Ruleset(flags, segments);
    }

    private static JsonElement? Property(JsonElement obj, string name) =>
        obj.TryGetProperty(name, out var value) ? value : null;

    /// <summary>First entry per key wins, even when it is the malformed one; an entry with no string key is dropped.</summary>
    private static Dictionary<string, T> IndexByKey<T>(JsonElement? items, Func<JsonElement, T> compile)
    {
        var index = new Dictionary<string, T>(StringComparer.Ordinal);
        if (items is not { ValueKind: JsonValueKind.Array } array) return index;
        foreach (var item in array.EnumerateArray())
        {
            if (item.ValueKind != JsonValueKind.Object) continue;
            if (!item.TryGetProperty("key", out var key) || key.ValueKind != JsonValueKind.String) continue;
            var name = JsonStrings.Read(key);
            if (!index.ContainsKey(name)) index[name] = compile(item);
        }
        return index;
    }

    private static FlagEntry CompileFlag(JsonElement value, Dictionary<string, Segment?> segments)
    {
        var header = Validation.ReadHeader(value);
        if (header is null) return new MalformedEntry();
        var flag = Validation.ReadBody(value, header);
        if (flag is null) return new BrokenEntry(header, ErrorCode.MalformedFlag);
        var problem = SegmentProblem(flag, segments);
        return problem is { } code ? new BrokenEntry(header, code) : new ReadyEntry(flag);
    }

    /// <summary>The first reference to a segment that is missing or malformed, in rule, clause, value order.</summary>
    private static ErrorCode? SegmentProblem(Flag flag, Dictionary<string, Segment?> segments)
    {
        foreach (var rule in flag.Rules)
        foreach (var clause in rule.Clauses)
        {
            if (clause.Op != Op.SegmentMatch) continue;
            foreach (var key in clause.SegmentKeys)
            {
                if (!segments.TryGetValue(key, out var segment)) return ErrorCode.SegmentNotFound;
                if (segment is null) return ErrorCode.MalformedSegment;
            }
        }
        return null;
    }
}

/// <summary>The shape of a ruleset, checked at runtime because rulesets arrive as JSON. docs/evaluation.md is normative.</summary>
internal static class Validation
{
    public static bool IsAbsent(JsonElement? value) => value is null or { ValueKind: JsonValueKind.Null or JsonValueKind.Undefined };

    private static JsonElement? Get(JsonElement obj, string name) =>
        obj.TryGetProperty(name, out var value) ? value : null;

    /// <summary>JSON numbers are binary64; <c>1e999</c> overflows to Infinity rather than failing.</summary>
    public static bool TryNumber(JsonElement value, out double number)
    {
        number = 0;
        if (value.ValueKind != JsonValueKind.Number) return false;
        if (value.TryGetDouble(out number)) return true;
        number = double.Parse(value.GetRawText(), NumberStyles.Float, CultureInfo.InvariantCulture);
        return true;
    }

    private static bool IsInteger(double number) => double.IsFinite(number) && number == Math.Floor(number);

    private static bool TryCount(JsonElement? value, out double count)
    {
        count = 0;
        return value is { } v && TryNumber(v, out count) && IsInteger(count) && count >= 0;
    }

    private static bool TryIndex(JsonElement? value, int variationCount, out int index)
    {
        index = 0;
        if (!TryCount(value, out var count) || count >= variationCount) return false;
        index = (int)count;
        return true;
    }

    private static bool TryKey(JsonElement? value, out string key)
    {
        key = "";
        if (value is not { ValueKind: JsonValueKind.String } v) return false;
        key = JsonStrings.Read(v);
        return key.Length > 0;
    }

    private static bool TryString(JsonElement? value, out string text)
    {
        text = "";
        if (value is not { ValueKind: JsonValueKind.String } v) return false;
        text = JsonStrings.Read(v);
        return true;
    }

    private static bool TryNegate(JsonElement? value, out bool negate)
    {
        negate = false;
        if (IsAbsent(value)) return true;
        if (value!.Value.ValueKind is not (JsonValueKind.True or JsonValueKind.False)) return false;
        negate = value.Value.ValueKind == JsonValueKind.True;
        return true;
    }

    private static bool TryStrings(JsonElement? value, out List<string> strings)
    {
        strings = [];
        if (value is not { ValueKind: JsonValueKind.Array } array) return false;
        foreach (var item in array.EnumerateArray())
        {
            if (!TryString(item, out var text)) return false;
            strings.Add(text);
        }
        return true;
    }

    private static bool TryClauseValue(JsonElement value, out ClauseValue clauseValue)
    {
        clauseValue = default;
        switch (value.ValueKind)
        {
            case JsonValueKind.String:
                clauseValue = new ClauseValue(ValueKind.String, JsonStrings.Read(value), 0, false);
                return true;
            case JsonValueKind.True or JsonValueKind.False:
                clauseValue = new ClauseValue(ValueKind.Boolean, "", 0, value.ValueKind == JsonValueKind.True);
                return true;
            case JsonValueKind.Number when TryNumber(value, out var number) && double.IsFinite(number):
                clauseValue = new ClauseValue(ValueKind.Number, "", number, false);
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
        _ => null,
    };

    private static string? OpName(JsonElement value) =>
        value.TryGetProperty("op", out var op) && op.ValueKind == JsonValueKind.String ? JsonStrings.Read(op) : null;

    private static Clause? ReadAttributeClause(JsonElement value)
    {
        if (value.ValueKind != JsonValueKind.Object || AttributeOp(OpName(value)) is not { } op) return null;
        if (!TryKey(Get(value, "attribute"), out var attribute)) return null;
        if (!TryNegate(Get(value, "negate"), out var negate)) return null;
        if (Get(value, "values") is not { ValueKind: JsonValueKind.Array } values) return null;
        var parsed = new List<ClauseValue>();
        foreach (var item in values.EnumerateArray())
        {
            if (!TryClauseValue(item, out var clauseValue)) return null;
            parsed.Add(clauseValue);
        }
        return new Clause(op, attribute, parsed, [], negate);
    }

    private static Clause? ReadSegmentClause(JsonElement value)
    {
        if (value.ValueKind != JsonValueKind.Object || OpName(value) != "segmentMatch") return null;
        if (!TryNegate(Get(value, "negate"), out var negate)) return null;
        if (!TryStrings(Get(value, "values"), out var keys)) return null;
        return new Clause(Op.SegmentMatch, "", [], keys, negate);
    }

    private static List<T>? ReadAll<T>(JsonElement? value, Func<JsonElement, T?> read) where T : class
    {
        if (value is not { ValueKind: JsonValueKind.Array } array) return null;
        var items = new List<T>();
        foreach (var item in array.EnumerateArray())
        {
            if (read(item) is not { } parsed) return null;
            items.Add(parsed);
        }
        return items;
    }

    private static bool TryRollout(JsonElement? value, int variationCount, out List<WeightedVariation> rollout)
    {
        rollout = [];
        if (value is not { ValueKind: JsonValueKind.Object } obj) return false;
        if (Get(obj, "variations") is not { ValueKind: JsonValueKind.Array } variations || variations.GetArrayLength() == 0) return false;
        double total = 0;
        foreach (var item in variations.EnumerateArray())
        {
            if (item.ValueKind != JsonValueKind.Object) return false;
            if (!TryIndex(Get(item, "variation"), variationCount, out var index)) return false;
            if (!TryCount(Get(item, "weight"), out var weight) || weight > Bucketing.BucketCount) return false;
            rollout.Add(new WeightedVariation(index, weight));
            total += weight;
        }
        return total == Bucketing.BucketCount;
    }

    /// <summary>Exactly one of <c>variation</c> or <c>rollout</c>.</summary>
    private static Serve? ReadServe(JsonElement value, int variationCount)
    {
        if (value.ValueKind != JsonValueKind.Object) return null;
        var variation = Get(value, "variation");
        var rollout = Get(value, "rollout");
        if (IsAbsent(rollout))
        {
            return TryIndex(variation, variationCount, out var index) ? new Serve(index, null) : null;
        }
        return IsAbsent(variation) && TryRollout(rollout, variationCount, out var weighted) ? new Serve(null, weighted) : null;
    }

    private static Rule? ReadRule(JsonElement value, int variationCount)
    {
        if (value.ValueKind != JsonValueKind.Object) return null;
        var clauses = ReadAll(Get(value, "clauses"), clause => ReadAttributeClause(clause) ?? ReadSegmentClause(clause));
        if (clauses is null || ReadServe(value, variationCount) is not { } serve) return null;
        return new Rule(clauses, serve);
    }

    /// <summary>What serving the off variation needs: key, on, variations and offVariation.</summary>
    public static FlagHeader? ReadHeader(JsonElement value)
    {
        if (value.ValueKind != JsonValueKind.Object) return null;
        var on = Get(value, "on");
        if (on is not { ValueKind: JsonValueKind.True or JsonValueKind.False }) return null;
        if (!TryKey(Get(value, "key"), out var key)) return null;
        if (Get(value, "variations") is not { ValueKind: JsonValueKind.Array } variations || variations.GetArrayLength() == 0) return null;
        var values = variations.EnumerateArray().Select(item => item.Clone()).ToList();
        var offVariation = Get(value, "offVariation");
        int? off = null;
        if (!IsAbsent(offVariation))
        {
            if (!TryIndex(offVariation, values.Count, out var index)) return null;
            off = index;
        }
        return new FlagHeader(key, on.Value.ValueKind == JsonValueKind.True, values, off);
    }

    /// <summary>The rest of the flag, or null when anything in it is invalid.</summary>
    public static Flag? ReadBody(JsonElement value, FlagHeader header)
    {
        var count = header.Variations.Count;
        if (!TryString(Get(value, "salt"), out var salt)) return null;
        if (!TryCount(Get(value, "version"), out _)) return null;

        var targets = new Dictionary<string, int>(StringComparer.Ordinal);
        var rawTargets = ReadAll(Get(value, "targets"), item => ReadTarget(item, count));
        if (rawTargets is null) return null;
        foreach (var target in rawTargets)
        foreach (var contextKey in target.ContextKeys)
        {
            targets.TryAdd(contextKey, target.Variation);
        }

        var prerequisites = ReadAll(Get(value, "prerequisites"), ReadPrerequisite);
        var rules = ReadAll(Get(value, "rules"), item => ReadRule(item, count));
        if (prerequisites is null || rules is null) return null;
        var fallthrough = Get(value, "fallthrough") is { } raw ? ReadServe(raw, count) : null;
        return fallthrough is null ? null : new Flag(header, salt, targets, prerequisites, rules, fallthrough);
    }

    private static Target? ReadTarget(JsonElement value, int variationCount)
    {
        if (value.ValueKind != JsonValueKind.Object) return null;
        if (!TryIndex(Get(value, "variation"), variationCount, out var variation)) return null;
        return TryStrings(Get(value, "contextKeys"), out var keys) ? new Target(variation, keys) : null;
    }

    private static Prerequisite? ReadPrerequisite(JsonElement value)
    {
        if (value.ValueKind != JsonValueKind.Object) return null;
        if (!TryKey(Get(value, "flagKey"), out var flagKey) || !TryCount(Get(value, "variation"), out var variation)) return null;
        return new Prerequisite(flagKey, variation);
    }

    private static SegmentRule? ReadSegmentRule(JsonElement value)
    {
        if (value.ValueKind != JsonValueKind.Object) return null;
        var weight = Get(value, "weight");
        double? parsedWeight = null;
        if (!IsAbsent(weight))
        {
            if (!TryCount(weight, out var count) || count > Bucketing.BucketCount) return null;
            parsedWeight = count;
        }
        var clauses = ReadAll(Get(value, "clauses"), ReadAttributeClause);
        return clauses is null ? null : new SegmentRule(clauses, parsedWeight);
    }

    /// <summary>A segment, or null when it is malformed.</summary>
    public static Segment? ReadSegment(JsonElement value)
    {
        if (value.ValueKind != JsonValueKind.Object) return null;
        if (!TryKey(Get(value, "key"), out var key) || !TryString(Get(value, "salt"), out var salt)) return null;
        if (!TryStrings(Get(value, "included"), out var included) || !TryStrings(Get(value, "excluded"), out var excluded)) return null;
        var rules = ReadAll(Get(value, "rules"), ReadSegmentRule);
        return rules is null ? null : new Segment(key, salt, included.ToHashSet(StringComparer.Ordinal), excluded.ToHashSet(StringComparer.Ordinal), rules);
    }
}
