using System.Text.Json;

namespace ToggleSlash.Sdk.Evaluation;

/// <summary>How the SDK reads JSON text: the options, the byte order mark and the shape of a ruleset.</summary>
internal static class JsonText
{
    /// <summary>A generous depth; a ruleset or a context nests nowhere near it.</summary>
    public static readonly JsonDocumentOptions Options = new() { MaxDepth = 256 };

    /// <summary>
    /// Parses a JSON body, skipping one leading UTF-8 byte order mark. Duplicate keys are kept as they came,
    /// and <see cref="JsonElement.TryGetProperty(string, out JsonElement)"/> answers the last of them.
    /// Throws <see cref="JsonException"/> when the body is not JSON.
    /// </summary>
    public static JsonDocument Parse(ReadOnlyMemory<byte> body)
    {
        if (body.Length >= 3 && body.Span[0] == 0xEF && body.Span[1] == 0xBB && body.Span[2] == 0xBF) body = body[3..];
        return JsonDocument.Parse(body, Options);
    }

    /// <summary>Only an object with a <c>flags</c> array and a <c>segments</c> array is a ruleset.</summary>
    public static bool HasRulesetShape(JsonElement root) =>
        root.ValueKind == JsonValueKind.Object
        && root.TryGetProperty("flags", out var flags) && flags.ValueKind == JsonValueKind.Array
        && root.TryGetProperty("segments", out var segments) && segments.ValueKind == JsonValueKind.Array;
}
