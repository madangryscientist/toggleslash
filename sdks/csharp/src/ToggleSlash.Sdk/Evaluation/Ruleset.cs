using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk.Evaluation;

/// <summary>
/// One environment's flags and segments, validated and indexed once so each evaluation skips that work. <c>Load</c>
/// never throws: a bad flag becomes an <c>ERROR</c> result when it is evaluated. The ruleset holds only immutable
/// data, so it is shared between threads without a lock.
/// </summary>
internal sealed class Ruleset
{
    private readonly Dictionary<string, FlagEntry> _flags;

    // A null segment is a malformed one.
    private readonly Dictionary<string, Segment?> _segments;

    private Ruleset(Dictionary<string, FlagEntry> flags, Dictionary<string, Segment?> segments)
    {
        _flags = flags;
        _segments = segments;
    }

    /// <summary>An empty ruleset.</summary>
    internal static Ruleset Empty { get; } = Load(JsonValue.Null);

    internal bool TryGetFlag(string key, out FlagEntry entry) => _flags.TryGetValue(key, out entry!);

    internal bool TryGetSegment(string key, out Segment? segment) => _segments.TryGetValue(key, out segment);

    /// <summary>
    /// The <c>version</c> of the flag when it is well formed enough to carry one: not for an unknown flag, one that is
    /// malformed by shape, or one that refers to a missing or malformed segment. A flag that only sits in a
    /// prerequisite cycle keeps its version.
    /// </summary>
    internal double? VersionOf(string key) =>
        _flags.TryGetValue(key, out var entry) && entry is ReadyEntry ready ? ready.Flag.Version : null;

    /// <summary>Loads a ruleset from its JSON form. A root that is not an object is an empty ruleset.</summary>
    internal static Ruleset Load(JsonValue root)
    {
        JsonValue? rawFlags = null, rawSegments = null;
        if (root is JsonObject obj)
        {
            rawFlags = Validation.Get(obj, "flags");
            rawSegments = Validation.Get(obj, "segments");
        }

        var segments = IndexByKey(rawSegments, Validation.ReadSegment);
        var flags = IndexByKey(rawFlags, item => CompileFlag(item, segments));
        return new Ruleset(flags, segments);
    }

    /// <summary>First entry per key wins, even when it is the malformed one; an entry with no string key is dropped.</summary>
    private static Dictionary<string, T> IndexByKey<T>(JsonValue? items, Func<JsonValue, T> compile)
    {
        var index = new Dictionary<string, T>(StringComparer.Ordinal);
        if (items is not JsonArray array)
        {
            return index;
        }

        foreach (var item in array.Items)
        {
            if (item is not JsonObject obj || !obj.TryGetValue("key", out var key) || key is not JsonString name)
            {
                continue;
            }

            if (!index.ContainsKey(name.Value))
            {
                index[name.Value] = compile(item);
            }
        }

        return index;
    }

    private static FlagEntry CompileFlag(JsonValue value, Dictionary<string, Segment?> segments)
    {
        var header = Validation.ReadHeader(value);
        if (header is null)
        {
            return new MalformedEntry();
        }

        var flag = Validation.ReadBody((JsonObject)value, header);
        if (flag is null)
        {
            return new BrokenEntry(header, ErrorCode.MALFORMED_FLAG);
        }

        return SegmentProblem(flag, segments) is { } code ? new BrokenEntry(header, code) : new ReadyEntry(flag);
    }

    /// <summary>The first reference to a segment that is missing or malformed, in rule, clause, value order.</summary>
    private static ErrorCode? SegmentProblem(Flag flag, Dictionary<string, Segment?> segments)
    {
        foreach (var rule in flag.Rules)
        {
            foreach (var clause in rule.Clauses)
            {
                if (clause.Op != Op.SegmentMatch)
                {
                    continue;
                }

                foreach (var key in clause.SegmentKeys)
                {
                    if (!segments.TryGetValue(key, out var segment))
                    {
                        return ErrorCode.SEGMENT_NOT_FOUND;
                    }

                    if (segment is null)
                    {
                        return ErrorCode.MALFORMED_SEGMENT;
                    }
                }
            }
        }

        return null;
    }
}
