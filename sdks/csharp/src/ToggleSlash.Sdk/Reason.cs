namespace ToggleSlash.Sdk;

/// <summary>Why a flag evaluated the way it did. The member names are the wire names in docs/evaluation.md.</summary>
public enum ReasonKind
{
    /// <summary>The flag is off.</summary>
    OFF,

    /// <summary>A direct prerequisite was not met.</summary>
    PREREQUISITE_FAILED,

    /// <summary>A target listed the context.</summary>
    TARGET_MATCH,

    /// <summary>A rule matched.</summary>
    RULE_MATCH,

    /// <summary>No target or rule matched.</summary>
    FALLTHROUGH,

    /// <summary>The evaluation could not finish; see <see cref="ErrorCode"/>.</summary>
    ERROR,
}

/// <summary>The code of a <see cref="ReasonKind.ERROR"/> reason. The member names are the wire names.</summary>
public enum ErrorCode
{
    /// <summary>The context is not valid.</summary>
    INVALID_CONTEXT,

    /// <summary>No such flag.</summary>
    FLAG_NOT_FOUND,

    /// <summary>The flag refers to a segment that is not in the ruleset.</summary>
    SEGMENT_NOT_FOUND,

    /// <summary>The flag is malformed.</summary>
    MALFORMED_FLAG,

    /// <summary>The flag refers to a segment that is malformed.</summary>
    MALFORMED_SEGMENT,

    /// <summary>Prerequisites form a cycle.</summary>
    PREREQUISITE_CYCLE,

    /// <summary>Prerequisites are nested too deep.</summary>
    PREREQUISITE_DEPTH_EXCEEDED,

    /// <summary>No ruleset has been loaded yet.</summary>
    NOT_READY,

    /// <summary>The served value is not the type the caller asked for.</summary>
    TYPE_MISMATCH,

    /// <summary>The evaluation raised.</summary>
    EVALUATION_FAILED,
}

/// <summary>
/// The reason half of a result. <see cref="RuleIndex"/> is set for <see cref="ReasonKind.RULE_MATCH"/>,
/// <see cref="FlagKey"/> for <see cref="ReasonKind.PREREQUISITE_FAILED"/> and <see cref="ErrorCode"/> for
/// <see cref="ReasonKind.ERROR"/>.
/// </summary>
/// <param name="Kind">What decided the result.</param>
/// <param name="RuleIndex">The index in <c>rules</c>, for a rule match.</param>
/// <param name="FlagKey">The direct prerequisite that failed.</param>
/// <param name="ErrorCode">The error, for an error reason.</param>
public sealed record Reason(ReasonKind Kind, int? RuleIndex = null, string? FlagKey = null, ErrorCode? ErrorCode = null)
{
    /// <summary>An error reason with <paramref name="code"/>.</summary>
    internal static Reason Error(ErrorCode code) => new(ReasonKind.ERROR, ErrorCode: code);
}
