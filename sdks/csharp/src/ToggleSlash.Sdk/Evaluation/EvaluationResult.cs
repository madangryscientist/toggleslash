using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk.Evaluation;

/// <summary>What a flag evaluated to, before the caller's accessor reads it.</summary>
/// <param name="Value">The served variation's JSON value (JSON null when nothing is served).</param>
/// <param name="VariationIndex">The served index, null when no variation was served at all.</param>
/// <param name="Reason">Why.</param>
internal sealed record EvaluationResult(JsonValue Value, int? VariationIndex, Reason Reason)
{
    internal static EvaluationResult Failure(ErrorCode code) => new(JsonValue.Null, null, Reason.Error(code));
}
