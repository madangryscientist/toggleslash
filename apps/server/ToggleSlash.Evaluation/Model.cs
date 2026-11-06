using System.Text.Json;

namespace ToggleSlash.Evaluation;

internal enum Op
{
    In,
    Contains,
    StartsWith,
    EndsWith,
    Lt,
    Lte,
    Gt,
    Gte,
    SemverEq,
    SemverLt,
    SemverLte,
    SemverGt,
    SemverGte,
    SegmentMatch,
}

internal enum ValueKind
{
    String,
    Number,
    Boolean,
}

/// <summary>A clause value: a string, a finite number or a boolean.</summary>
internal readonly record struct ClauseValue(ValueKind Kind, string String, double Number, bool Boolean);

/// <summary>An attribute clause or a segment clause. A segment clause's values are segment keys.</summary>
internal sealed record Clause(Op Op, string Attribute, IReadOnlyList<ClauseValue> Values, IReadOnlyList<string> SegmentKeys, bool Negate);

internal sealed record WeightedVariation(int Variation, double Weight);

/// <summary>Exactly one of <see cref="Variation"/> or <see cref="Rollout"/> is set.</summary>
internal sealed record Serve(int? Variation, IReadOnlyList<WeightedVariation>? Rollout);

internal sealed record Rule(IReadOnlyList<Clause> Clauses, Serve Serve);

internal sealed record Target(int Variation, IReadOnlyList<string> ContextKeys);

internal sealed record Prerequisite(string FlagKey, double Variation);

/// <summary>What serving the off variation needs; checked apart so <c>on: false</c> survives a bad rule.</summary>
internal sealed record FlagHeader(string Key, bool On, IReadOnlyList<JsonElement> Variations, int? OffVariation);

internal sealed record Flag(
    FlagHeader Header,
    string Salt,
    IReadOnlyDictionary<string, int> Targets,
    IReadOnlyList<Prerequisite> Prerequisites,
    IReadOnlyList<Rule> Rules,
    Serve Fallthrough);

internal sealed record SegmentRule(IReadOnlyList<Clause> Clauses, double? Weight);

internal sealed record Segment(
    string Key,
    string Salt,
    IReadOnlySet<string> Included,
    IReadOnlySet<string> Excluded,
    IReadOnlyList<SegmentRule> Rules);

internal abstract record FlagEntry;

/// <summary>The header is invalid: not even the off variation can be served.</summary>
internal sealed record MalformedEntry : FlagEntry;

/// <summary>The header is fine but the body is not, or a segment it refers to is missing or malformed.</summary>
internal sealed record BrokenEntry(FlagHeader Header, ErrorCode Code) : FlagEntry;

internal sealed record ReadyEntry(Flag Flag) : FlagEntry;
