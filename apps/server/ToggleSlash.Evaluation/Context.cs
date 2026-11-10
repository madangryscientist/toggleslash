using System.Text.Json;

namespace ToggleSlash.Evaluation;

/// <summary>The key and attributes of one kind, after normalising. <see cref="KeyElement"/> is the key as JSON.</summary>
internal sealed record KindContext(string Key, JsonElement KeyElement, JsonElement? Attributes);

/// <summary>Context kinds and the two context shapes, to "Context kinds" and "Context" in docs/evaluation.md.</summary>
internal static class ContextKinds
{
    /// <summary>What a kind that is read and absent means, everywhere.</summary>
    public const string Default = "user";

    /// <summary><c>^[A-Za-z0-9._-]+$</c> and neither <c>kind</c> nor <c>multi</c>, by a walk over the characters.</summary>
    public static bool IsKind(string text)
    {
        if (text.Length == 0 || text is "kind" or "multi") return false;
        foreach (var c in text)
        {
            if (c is not ((>= 'a' and <= 'z') or (>= 'A' and <= 'Z') or (>= '0' and <= '9') or '.' or '_' or '-')) return false;
        }
        return true;
    }

    /// <summary>The context as a map from kind to its key and attributes, or null when it is not valid.</summary>
    public static IReadOnlyDictionary<string, KindContext>? Normalise(JsonElement context)
    {
        if (context.ValueKind != JsonValueKind.Object) return null;
        var kind = Default;
        if (Get(context, "kind") is { } rawKind)
        {
            if (rawKind.ValueKind != JsonValueKind.String) return null;
            kind = JsonStrings.Read(rawKind);
            if (kind == "multi") return ReadMulti(context);
            if (!IsKind(kind)) return null;
        }
        return ReadEntry(context) is { } entry
            ? new Dictionary<string, KindContext>(StringComparer.Ordinal) { [kind] = entry }
            : null;
    }

    /// <summary>
    /// The bucket for the key of <paramref name="kind"/> (null is <c>user</c>): the key as it is for <c>user</c>,
    /// <c>kind:key</c> for any other kind, and 0 when the context does not have that kind.
    /// </summary>
    public static int Bucket(string key, string salt, string? kind, IReadOnlyDictionary<string, KindContext> contexts)
    {
        var chosen = kind ?? Default;
        if (!contexts.TryGetValue(chosen, out var context)) return 0;
        return Bucketing.Bucket(key, salt, chosen == Default ? context.Key : $"{chosen}:{context.Key}");
    }

    /// <summary>A property that is present and not null; null and absent are the same.</summary>
    private static JsonElement? Get(JsonElement obj, string name) =>
        obj.TryGetProperty(name, out var value) && value.ValueKind != JsonValueKind.Null ? value : null;

    private static KindContext? ReadEntry(JsonElement value)
    {
        if (Get(value, "key") is not { ValueKind: JsonValueKind.String } rawKey) return null;
        var key = JsonStrings.Read(rawKey);
        if (key.Length == 0) return null;
        var attributes = Get(value, "attributes");
        if (attributes is { } present && present.ValueKind != JsonValueKind.Object) return null;
        return new KindContext(key, rawKey, attributes);
    }

    private static Dictionary<string, KindContext>? ReadMulti(JsonElement context)
    {
        if (Get(context, "key") is not null || Get(context, "attributes") is not null) return null;
        if (Get(context, "contexts") is not { ValueKind: JsonValueKind.Object } contexts) return null;
        var normalised = new Dictionary<string, KindContext>(StringComparer.Ordinal);
        foreach (var property in contexts.EnumerateObject())
        {
            // A name the reader cannot decode holds a lone surrogate, so it is not a kind either; never throw.
            string name;
            try { name = property.Name; }
            catch (InvalidOperationException) { return null; }
            var value = property.Value;
            if (!IsKind(name) || value.ValueKind != JsonValueKind.Object || Get(value, "kind") is not null) return null;
            if (ReadEntry(value) is not { } entry) return null;
            normalised[name] = entry;
        }
        return normalised.Count > 0 ? normalised : null;
    }
}
