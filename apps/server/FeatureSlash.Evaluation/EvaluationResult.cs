using System.Text.Json;

namespace FeatureSlash.Evaluation;

/// <summary>Why a flag evaluated the way it did. The wire names are the ones in docs/evaluation.md.</summary>
public enum ReasonKind
{
    Off,
    TargetMatch,
    RuleMatch,
    Fallthrough,
    PrerequisiteFailed,
    Error,
}

public enum ErrorCode
{
    InvalidContext,
    FlagNotFound,
    SegmentNotFound,
    MalformedFlag,
    MalformedSegment,
    PrerequisiteCycle,
    PrerequisiteDepthExceeded,
}

/// <summary>
/// The reason half of a result. <see cref="RuleIndex"/> is set for <see cref="ReasonKind.RuleMatch"/>,
/// <see cref="FlagKey"/> for <see cref="ReasonKind.PrerequisiteFailed"/> and <see cref="Code"/> for
/// <see cref="ReasonKind.Error"/>.
/// </summary>
public sealed record Reason(
    ReasonKind Kind,
    int? RuleIndex = null,
    string? FlagKey = null,
    ErrorCode? Code = null)
{
    /// <summary>The spec's name for <see cref="Kind"/>, e.g. <c>RULE_MATCH</c>.</summary>
    public string KindName => Kind switch
    {
        ReasonKind.Off => "OFF",
        ReasonKind.TargetMatch => "TARGET_MATCH",
        ReasonKind.RuleMatch => "RULE_MATCH",
        ReasonKind.Fallthrough => "FALLTHROUGH",
        ReasonKind.PrerequisiteFailed => "PREREQUISITE_FAILED",
        _ => "ERROR",
    };

    /// <summary>The spec's name for <see cref="Code"/>, e.g. <c>FLAG_NOT_FOUND</c>; null unless this is an error.</summary>
    public string? CodeName => Code switch
    {
        null => null,
        ErrorCode.InvalidContext => "INVALID_CONTEXT",
        ErrorCode.FlagNotFound => "FLAG_NOT_FOUND",
        ErrorCode.SegmentNotFound => "SEGMENT_NOT_FOUND",
        ErrorCode.MalformedFlag => "MALFORMED_FLAG",
        ErrorCode.MalformedSegment => "MALFORMED_SEGMENT",
        ErrorCode.PrerequisiteCycle => "PREREQUISITE_CYCLE",
        _ => "PREREQUISITE_DEPTH_EXCEEDED",
    };
}

/// <summary>
/// What a flag evaluated to. <see cref="Value"/> is the served variation's JSON value (a JSON
/// <c>null</c> when nothing is served), and <see cref="VariationIndex"/> is null when no variation
/// was served at all, which tells that apart from a variation whose value is <c>null</c>.
/// </summary>
public sealed record EvaluationResult(JsonElement Value, int? VariationIndex, Reason Reason)
{
    internal static readonly JsonElement JsonNull = JsonDocument.Parse("null").RootElement.Clone();

    internal static EvaluationResult Failure(ErrorCode code) =>
        new(JsonNull, null, new Reason(ReasonKind.Error, Code: code));
}
