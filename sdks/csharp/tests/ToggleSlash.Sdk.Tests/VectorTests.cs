using ToggleSlash.Sdk.Evaluation;
using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk.Tests;

/// <summary>
/// Runs every shared evaluation vector and bucket pin from fixtures/evaluation/ (read at test time, never copied)
/// through the evaluator, and prints the counts so bin/test can be compared with the phase 2 count command.
/// </summary>
public sealed class VectorTests
{
    private static string FixtureDirectory()
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir is not null; dir = dir.Parent)
        {
            var candidate = Path.Combine(dir.FullName, "fixtures", "evaluation");
            if (Directory.Exists(candidate))
            {
                return candidate;
            }
        }

        throw new DirectoryNotFoundException("fixtures/evaluation was not found above " + AppContext.BaseDirectory);
    }

    private static JsonValue ReasonJson(Reason reason)
    {
        var properties = new List<KeyValuePair<string, JsonValue>> { new("kind", JsonValue.From(reason.Kind.ToString())) };
        if (reason.RuleIndex is { } index)
        {
            properties.Add(new("ruleIndex", JsonValue.From(index)));
        }

        if (reason.FlagKey is { } flagKey)
        {
            properties.Add(new("flagKey", JsonValue.From(flagKey)));
        }

        if (reason.ErrorCode is { } code)
        {
            properties.Add(new("code", JsonValue.From(code.ToString())));
        }

        return new JsonObject(properties);
    }

    private static JsonValue Read(string path) =>
        JsonValue.Parse(File.ReadAllText(path)) ?? throw new InvalidDataException(path + " is not JSON");

    [Fact]
    public void EveryVectorAndPinPasses()
    {
        var directory = FixtureDirectory();
        var failures = new List<string>();
        var vectors = 0;
        var pins = 0;

        foreach (var file in Directory.GetFiles(directory, "*.json").Order(StringComparer.Ordinal))
        {
            var root = (JsonObject)Read(file);
            if (Path.GetFileName(file) == "buckets.json")
            {
                foreach (var pin in ((JsonArray)root.Properties["buckets"]).Items.Cast<JsonObject>())
                {
                    pins++;
                    var flagKey = ((JsonString)pin.Properties["flagKey"]).Value;
                    var salt = ((JsonString)pin.Properties["salt"]).Value;
                    var contextKey = ((JsonString)pin.Properties["contextKey"]).Value;
                    var uint32 = ((JsonNumber)pin.Properties["uint32"]).Value;
                    var bucket = ((JsonNumber)pin.Properties["bucket"]).Value;
                    if (Bucketing.Hash(flagKey, salt, contextKey) != uint32 || Bucketing.Bucket(flagKey, salt, contextKey) != bucket)
                    {
                        failures.Add($"pin {flagKey}/{salt}/{contextKey}");
                    }
                }

                continue;
            }

            foreach (var vector in ((JsonArray)root.Properties["vectors"]).Items.Cast<JsonObject>())
            {
                vectors++;
                var name = Path.GetFileName(file) + ": " + ((JsonString)vector.Properties["name"]).Value;
                var ruleset = Ruleset.Load(new JsonObject(
                [
                    new("flags", vector.Properties["flags"]),
                    new("segments", vector.Properties["segments"]),
                ]));
                var result = Evaluator.Evaluate(ruleset, ((JsonString)vector.Properties["flagKey"]).Value, vector.Properties["context"]);
                var expected = (JsonObject)vector.Properties["expected"];
                var expectedIndex = expected.Properties["variationIndex"];
                var actualIndex = result.VariationIndex is { } i ? JsonValue.From(i) : JsonValue.Null;
                if (!JsonValue.DeepEquals(expected.Properties["value"], result.Value)
                    || !JsonValue.DeepEquals(expectedIndex, actualIndex)
                    || !JsonValue.DeepEquals(expected.Properties["reason"], ReasonJson(result.Reason)))
                {
                    failures.Add($"{name}: got {result.Value} / {actualIndex} / {ReasonJson(result.Reason)}");
                }
            }
        }

        Console.WriteLine($"vectors: {vectors} pins: {pins}");
        Assert.True(vectors > 0 && pins > 0, "no vectors or pins were read");
        Assert.True(failures.Count == 0, string.Join(Environment.NewLine, failures.Take(20)));
    }
}
