using System.Text.Json;
using FeatureSlash.Evaluation;

/// <summary>
/// Runs every vector in <c>fixtures/evaluation/</c> against the C# evaluator, unmodified. The same
/// files run in <c>packages/engine/test/vectors.test.ts</c>. A vector that fails here is a bug in this
/// evaluator or in docs/evaluation.md, never a reason to special-case the vector.
/// </summary>
public sealed class VectorTests
{
    public static IEnumerable<object[]> AllVectors() =>
        Fixtures.VectorFiles.SelectMany(file =>
            Fixtures.Vectors(file).EnumerateArray().Select(vector => new object[] { file, vector.GetProperty("name").GetString()! }));

    [Theory]
    [MemberData(nameof(AllVectors))]
    public void Vector_evaluates_as_expected(string file, string name)
    {
        var vector = Fixtures.Vector(file, name);
        var ruleset = FeatureSlash.Evaluation.Ruleset.Load(vector);

        var result = Evaluator.Evaluate(ruleset, vector.GetProperty("flagKey").GetString()!, vector.GetProperty("context"));

        AssertMatches(vector.GetProperty("expected"), result);
    }

    private static void AssertMatches(JsonElement expected, EvaluationResult actual)
    {
        var reason = expected.GetProperty("reason");
        Assert.Equal(reason.GetProperty("kind").GetString(), actual.Reason.KindName);
        Assert.Equal(StringOrNull(reason, "code"), actual.Reason.CodeName);
        Assert.Equal(StringOrNull(reason, "flagKey"), actual.Reason.FlagKey);
        Assert.Equal(
            reason.TryGetProperty("ruleIndex", out var ruleIndex) ? ruleIndex.GetInt32() : null,
            actual.Reason.RuleIndex);

        var index = expected.GetProperty("variationIndex");
        Assert.Equal(index.ValueKind == JsonValueKind.Null ? null : index.GetInt32(), actual.VariationIndex);
        Assert.True(JsonElement.DeepEquals(expected.GetProperty("value"), actual.Value), $"value: expected {expected.GetProperty("value")}, got {actual.Value}");
    }

    private static string? StringOrNull(JsonElement obj, string name) =>
        obj.TryGetProperty(name, out var value) ? value.GetString() : null;
}
