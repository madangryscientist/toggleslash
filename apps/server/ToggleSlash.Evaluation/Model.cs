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
    Before,
    After,
    Matches,
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

/// <summary>
/// An attribute clause or a segment clause. A segment clause's values are segment keys. <see cref="ContextKind"/>
/// is the kind an attribute clause reads, null for <c>user</c>.
/// </summary>
internal sealed record Clause(Op Op, string Attribute, IReadOnlyList<ClauseValue> Values, IReadOnlyList<string> SegmentKeys, bool Negate, string? ContextKind = null);

internal sealed record WeightedVariation(int Variation, double Weight);

/// <summary>Exactly one of <see cref="Variation"/> or <see cref="Rollout"/> is set. <see cref="RolloutKind"/> is the kind a rollout buckets by, null for <c>user</c>.</summary>
internal sealed record Serve(int? Variation, IReadOnlyList<WeightedVariation>? Rollout, string? RolloutKind = null);

internal sealed record Rule(IReadOnlyList<Clause> Clauses, Serve Serve);

internal sealed record Target(int Variation, IReadOnlyList<string> ContextKeys, string Kind);

/// <summary>A target that lists a key: its place in <c>targets</c> and the variation it serves.</summary>
internal readonly record struct TargetHit(int Order, int Variation);

internal sealed record Prerequisite(string FlagKey, double Variation);

/// <summary>What serving the off variation needs; checked apart so <c>on: false</c> survives a bad rule.</summary>
internal sealed record FlagHeader(string Key, bool On, IReadOnlyList<JsonElement> Variations, int? OffVariation);

internal sealed record Flag(
    FlagHeader Header,
    string Salt,
    // By kind, then by key; the first target in array order holds each key.
    IReadOnlyDictionary<string, Dictionary<string, TargetHit>> Targets,
    IReadOnlyList<Prerequisite> Prerequisites,
    IReadOnlyList<Rule> Rules,
    Serve Fallthrough);

internal sealed record SegmentRule(IReadOnlyList<Clause> Clauses, double? Weight, string? RolloutKind);

/// <summary><see cref="Included"/> and <see cref="Excluded"/> are keys by kind: <c>included</c> under <c>user</c>, merged with the context lists.</summary>
internal sealed record Segment(
    string Key,
    string Salt,
    IReadOnlyDictionary<string, HashSet<string>> Included,
    IReadOnlyDictionary<string, HashSet<string>> Excluded,
    IReadOnlyList<SegmentRule> Rules);

internal abstract record FlagEntry;

/// <summary>The header is invalid: not even the off variation can be served.</summary>
internal sealed record MalformedEntry : FlagEntry;

/// <summary>The header is fine but the body is not, or a segment it refers to is missing or malformed.</summary>
internal sealed record BrokenEntry(FlagHeader Header, ErrorCode Code) : FlagEntry;

internal sealed record ReadyEntry(Flag Flag) : FlagEntry;
