using System.Text.Json;

/// <summary>The language-neutral vectors in <c>fixtures/evaluation/</c>, read with a real JSON parser.</summary>
internal static class Fixtures
{
    private static readonly Lazy<string> Directory = new(Find);

    // Parsed once for the run; the elements live as long as the documents.
    private static readonly Lazy<Dictionary<string, JsonDocument>> Documents = new(() =>
        System.IO.Directory.GetFiles(Directory.Value, "*.json")
            .Order(StringComparer.Ordinal)
            .ToDictionary(file => Path.GetFileName(file), file => JsonDocument.Parse(File.ReadAllText(file)))!);

    private static string Find()
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir is not null; dir = dir.Parent)
        {
            var candidate = Path.Combine(dir.FullName, "fixtures", "evaluation");
            if (System.IO.Directory.Exists(candidate)) return candidate;
        }
        throw new DirectoryNotFoundException("fixtures/evaluation not found above " + AppContext.BaseDirectory);
    }

    /// <summary>Every file but <c>buckets.json</c> holds <c>{ "vectors": [...] }</c>.</summary>
    public static IReadOnlyList<string> VectorFiles => [.. Documents.Value.Keys.Where(file => file != "buckets.json")];

    public static JsonElement Vectors(string file) => Documents.Value[file].RootElement.GetProperty("vectors");

    public static JsonElement Vector(string file, string name) =>
        Vectors(file).EnumerateArray().Single(vector => vector.GetProperty("name").GetString() == name);

    public static JsonElement Buckets() => Documents.Value["buckets.json"].RootElement.GetProperty("buckets");
}
