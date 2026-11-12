using System.Collections;
using System.Numerics;
using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk;

/// <summary>
/// Turns the context a caller passed into a <see cref="JsonValue"/> for the evaluator. A <see cref="JsonValue"/> is used as
/// it is. A native value is read this way: a dictionary with string keys is an object, an <see cref="IEnumerable"/> is an
/// array, a number of any C# numeric type is the JSON number of the same value (binary64), and a value with no JSON form
/// makes the whole context invalid (it becomes JSON null, which the evaluator answers <c>INVALID_CONTEXT</c>).
/// </summary>
internal static class ContextConverter
{
    private const int MaxDepth = 512;

    internal static JsonValue Convert(object? context) => TryConvert(context, 0) ?? JsonValue.Null;

    private static JsonValue? TryConvert(object? value, int depth)
    {
        if (depth > MaxDepth)
        {
            return null;
        }

        switch (value)
        {
            case null:
                return JsonValue.Null;
            case JsonValue json:
                return json;
            case string text:
                return new JsonString(text);
            case bool flag:
                return JsonValue.From(flag);
            case sbyte or byte or short or ushort or int or uint or long or ulong or float or double or decimal or nint or nuint
                or Half or Int128 or UInt128 or BigInteger:
                return new JsonNumber(ToDouble(value));
            case IReadOnlyDictionary<string, object?> readOnly:
                return Object(readOnly.Select(pair => new KeyValuePair<string, object?>(pair.Key, pair.Value)), depth);
            case IDictionary dictionary:
                return ObjectFromDictionary(dictionary, depth);
            case IEnumerable sequence:
                return Array(sequence, depth);
            default:
                return null;
        }
    }

    private static double ToDouble(object value) => value switch
    {
        sbyte v => v,
        byte v => v,
        short v => v,
        ushort v => v,
        int v => v,
        uint v => v,
        long v => v,
        ulong v => v,
        float v => v,
        double v => v,
        decimal v => (double)v,
        nint v => v,
        nuint v => v,
        Half v => (double)v,
        Int128 v => (double)v,
        UInt128 v => (double)v,
        BigInteger v => (double)v,
        _ => double.NaN,
    };

    private static JsonObject? ObjectFromDictionary(IDictionary dictionary, int depth)
    {
        var pairs = new List<KeyValuePair<string, object?>>(dictionary.Count);
        foreach (DictionaryEntry entry in dictionary)
        {
            if (entry.Key is not string key)
            {
                return null;
            }

            pairs.Add(new(key, entry.Value));
        }

        return Object(pairs, depth);
    }

    private static JsonObject? Object(IEnumerable<KeyValuePair<string, object?>> pairs, int depth)
    {
        var properties = new List<KeyValuePair<string, JsonValue>>();
        foreach (var pair in pairs)
        {
            var converted = TryConvert(pair.Value, depth + 1);
            if (converted is null)
            {
                return null;
            }

            properties.Add(new(pair.Key, converted));
        }

        return new JsonObject(properties);
    }

    private static JsonArray? Array(IEnumerable sequence, int depth)
    {
        var items = new List<JsonValue>();
        foreach (var item in sequence)
        {
            var converted = TryConvert(item, depth + 1);
            if (converted is null)
            {
                return null;
            }

            items.Add(converted);
        }

        return new JsonArray(items);
    }
}
