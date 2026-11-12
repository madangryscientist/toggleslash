using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk.Evaluation;

/// <summary>The key and attributes of one kind, after normalising. KeyValue is the key as JSON.</summary>
internal sealed record KindContext(string Key, JsonString KeyValue, JsonObject? Attributes);

/// <summary>Context kinds and the two context shapes, to "Context kinds" and "Context" in docs/evaluation.md.</summary>
internal static class ContextKinds
{
    /// <summary>What a kind that is read and absent means, everywhere.</summary>
    public const string Default = "user";

    /// <summary><c>^[A-Za-z0-9._-]+$</c> and neither <c>kind</c> nor <c>multi</c>, by a walk over the characters.</summary>
    public static bool IsKind(string text)
    {
        if (text.Length == 0 || text is "kind" or "multi")
        {
            return false;
        }

        foreach (var c in text)
        {
            if (c is not ((>= 'a' and <= 'z') or (>= 'A' and <= 'Z') or (>= '0' and <= '9') or '.' or '_' or '-'))
            {
                return false;
            }
        }

        return true;
    }

    /// <summary>The context as a map from kind to its key and attributes, or null when it is not valid.</summary>
    public static IReadOnlyDictionary<string, KindContext>? Normalise(JsonValue? context)
    {
        if (context is not JsonObject obj)
        {
            return null;
        }

        var kind = Default;
        if (Get(obj, "kind") is { } rawKind)
        {
            if (rawKind is not JsonString text)
            {
                return null;
            }

            kind = text.Value;
            if (kind == "multi")
            {
                return ReadMulti(obj);
            }

            if (!IsKind(kind))
            {
                return null;
            }
        }

        return ReadEntry(obj) is { } entry
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
        if (!contexts.TryGetValue(chosen, out var context))
        {
            return 0;
        }

        return Bucketing.Bucket(key, salt, chosen == Default ? context.Key : $"{chosen}:{context.Key}");
    }

    /// <summary>A property that is present and not null; null and absent are the same.</summary>
    private static JsonValue? Get(JsonObject obj, string name) =>
        obj.TryGetValue(name, out var value) && value is not JsonNull ? value : null;

    private static KindContext? ReadEntry(JsonObject value)
    {
        if (Get(value, "key") is not JsonString { Value.Length: > 0 } rawKey)
        {
            return null;
        }

        var attributes = Get(value, "attributes");
        if (attributes is not null and not JsonObject)
        {
            return null;
        }

        return new KindContext(rawKey.Value, rawKey, (JsonObject?)attributes);
    }

    private static Dictionary<string, KindContext>? ReadMulti(JsonObject context)
    {
        if (Get(context, "key") is not null || Get(context, "attributes") is not null)
        {
            return null;
        }

        if (Get(context, "contexts") is not JsonObject contexts)
        {
            return null;
        }

        var normalised = new Dictionary<string, KindContext>(StringComparer.Ordinal);
        foreach (var (name, value) in contexts.Properties)
        {
            if (!IsKind(name) || value is not JsonObject entryObject || Get(entryObject, "kind") is not null)
            {
                return null;
            }

            if (ReadEntry(entryObject) is not { } entry)
            {
                return null;
            }

            normalised[name] = entry;
        }

        return normalised.Count > 0 ? normalised : null;
    }
}
