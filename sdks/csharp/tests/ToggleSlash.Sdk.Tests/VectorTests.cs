using System.Text;
using System.Text.Json;
using ToggleSlash.Sdk.Evaluation;

namespace ToggleSlash.Sdk.Tests;

/// <summary>
/// Runs every vector in <c>fixtures/evaluation/</c> and every pin in <c>buckets.json</c> against the SDK's
/// evaluator, read from the repository, never from a copy. A vector that fails is a bug in the evaluator or in
/// docs/evaluation.md, never a reason to special-case the vector. The counts go to the file named by
/// <c>TOGGLESLASH_COUNTS_FILE</c> (when set) for <c>bin/test</c> to print.
/// </summary>
public sealed class VectorTests
{
    private static string FixtureDirectory()
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir is not null; dir = dir.Parent)
        {
            var candidate = Path.Combine(dir.FullName, "fixtures", "evaluation");
            if (Directory.Exists(candidate)) return candidate;
        }
        throw new DirectoryNotFoundException("fixtures/evaluation not found above " + AppContext.BaseDirectory);
    }

    private static JsonDocument Read(string file) => JsonText.Parse(File.ReadAllBytes(file));

    [Fact]
    public void Every_vector_and_every_pin_matches()
    {
        var failures = new List<string>();
        var vectors = 0;
        var pins = 0;
        var files = Directory.GetFiles(FixtureDirectory(), "*.json").Order(StringComparer.Ordinal).ToList();
        Assert.NotEmpty(files);

        foreach (var file in files)
        {
            var name = Path.GetFileName(file);
            using var document = Read(file);
            if (name == "buckets.json")
            {
                foreach (var pin in document.RootElement.GetProperty("buckets").EnumerateArray())
                {
                    pins++;
                    CheckPin(name, pin, failures);
                }
                continue;
            }
            foreach (var vector in document.RootElement.GetProperty("vectors").EnumerateArray())
            {
                vectors++;
                CheckVector(name, vector, failures);
            }
        }

        var counts = $"vectors: {vectors} pins: {pins}";
        if (Environment.GetEnvironmentVariable("TOGGLESLASH_COUNTS_FILE") is { Length: > 0 } path) File.WriteAllText(path, counts + "\n");
        Assert.True(vectors > 0, "no vectors were read");
        Assert.True(pins > 0, "no bucket pins were read");
        Assert.True(failures.Count == 0, counts + "\n" + string.Join("\n", failures));
    }

    private static void CheckPin(string file, JsonElement pin, List<string> failures)
    {
        var flagKey = pin.GetProperty("flagKey").GetString()!;
        var salt = pin.GetProperty("salt").GetString()!;
        var contextKey = pin.GetProperty("contextKey").GetString()!;
        var label = $"{file}: {flagKey}.{salt}.{contextKey}";
        var hash = Bucketing.Hash(flagKey, salt, contextKey);
        if (hash != pin.GetProperty("uint32").GetUInt32()) failures.Add($"{label}: uint32 {hash}, expected {pin.GetProperty("uint32")}");
        var bucket = Bucketing.Bucket(flagKey, salt, contextKey);
        if (bucket != pin.GetProperty("bucket").GetInt32()) failures.Add($"{label}: bucket {bucket}, expected {pin.GetProperty("bucket")}");
    }

    private static void CheckVector(string file, JsonElement vector, List<string> failures)
    {
        var label = $"{file}: {vector.GetProperty("name").GetString()}";
        try
        {
            var ruleset = Ruleset.Load(vector);
            var result = Evaluator.Evaluate(ruleset, vector.GetProperty("flagKey").GetString()!, vector.GetProperty("context"));
            var expected = vector.GetProperty("expected");
            var problem = Compare(expected, result);
            if (problem is not null) failures.Add($"{label}: {problem}");
        }
        catch (Exception error)
        {
            failures.Add($"{label}: raised {error.GetType().Name}: {error.Message}");
        }
    }

    private static string? Compare(JsonElement expected, EvaluationResult actual)
    {
        var problems = new StringBuilder();
        var reason = expected.GetProperty("reason");
        Check(problems, "reason.kind", reason.GetProperty("kind").GetString(), actual.Reason.KindName);
        Check(problems, "reason.code", StringOrNull(reason, "code"), actual.Reason.CodeName);
        Check(problems, "reason.flagKey", StringOrNull(reason, "flagKey"), actual.Reason.FlagKey);
        Check(problems, "reason.ruleIndex", reason.TryGetProperty("ruleIndex", out var ruleIndex) ? ruleIndex.GetInt32() : null, actual.Reason.RuleIndex);
        var index = expected.GetProperty("variationIndex");
        Check(problems, "variationIndex", index.ValueKind == JsonValueKind.Null ? null : index.GetInt32(), actual.VariationIndex);
        if (!SpecEquals(expected.GetProperty("value"), actual.Value))
            problems.Append($"value: expected {expected.GetProperty("value")}, got {actual.Value}; ");
        return problems.Length == 0 ? null : problems.ToString();
    }

    private static void Check<T>(StringBuilder problems, string what, T expected, T actual)
    {
        if (!EqualityComparer<T>.Default.Equals(expected, actual)) problems.Append($"{what}: expected {expected}, got {actual}; ");
    }

    private static string? StringOrNull(JsonElement obj, string name) =>
        obj.TryGetProperty(name, out var value) && value.ValueKind == JsonValueKind.String ? JsonStrings.Read(value) : null;

    /// <summary>The spec's equality: numbers by IEEE-754 (so -0 equals 0), strings by code units, objects by key.</summary>
    internal static bool SpecEquals(JsonElement a, JsonElement b)
    {
        if (a.ValueKind is JsonValueKind.True or JsonValueKind.False) return a.ValueKind == b.ValueKind;
        if (a.ValueKind != b.ValueKind) return false;
        switch (a.ValueKind)
        {
            case JsonValueKind.Number:
                return Validation.TryNumber(a, out var x) && Validation.TryNumber(b, out var y) && x == y;
            case JsonValueKind.String:
                return JsonStrings.Read(a) == JsonStrings.Read(b);
            case JsonValueKind.Array:
                return a.GetArrayLength() == b.GetArrayLength()
                    && a.EnumerateArray().Zip(b.EnumerateArray()).All(pair => SpecEquals(pair.First, pair.Second));
            case JsonValueKind.Object:
                var left = a.EnumerateObject().ToDictionary(p => p.Name, p => p.Value, StringComparer.Ordinal);
                var right = b.EnumerateObject().ToDictionary(p => p.Name, p => p.Value, StringComparer.Ordinal);
                return left.Count == right.Count && left.All(p => right.TryGetValue(p.Key, out var other) && SpecEquals(p.Value, other));
            default:
                return true;
        }
    }
}
