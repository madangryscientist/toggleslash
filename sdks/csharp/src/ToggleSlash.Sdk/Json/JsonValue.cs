using System.Globalization;
using System.Text;

namespace ToggleSlash.Sdk.Json;

/// <summary>
/// A JSON value, held the way the evaluation spec reads one: numbers are IEEE-754 binary64
/// (<c>1e999</c> is Infinity, <c>9007199254740993</c> is <c>9007199254740992</c>), and a string is a .NET string,
/// so a lone surrogate (<c>"\ud800"</c>) is kept as it is. Values are immutable.
/// </summary>
public abstract class JsonValue
{
    private protected JsonValue()
    {
    }

    /// <summary>The JSON <c>null</c>.</summary>
    public static JsonValue Null { get; } = new JsonNull();

    /// <summary>The JSON <c>true</c>.</summary>
    public static JsonValue True { get; } = new JsonBool(true);

    /// <summary>The JSON <c>false</c>.</summary>
    public static JsonValue False { get; } = new JsonBool(false);

    /// <summary>Parses JSON text, or returns null when it is not valid (nesting past 512 levels is not accepted).</summary>
    public static JsonValue? Parse(string text) => JsonParser.Parse(text);

    /// <summary>A JSON boolean.</summary>
    public static JsonValue From(bool value) => value ? True : False;

    /// <summary>A JSON number.</summary>
    public static JsonValue From(double value) => new JsonNumber(value);

    /// <summary>A JSON string; a null string is the JSON null.</summary>
    public static JsonValue From(string? value) => value is null ? Null : new JsonString(value);

    /// <summary>The text of this value as JSON. A number that is not finite is written as <c>null</c>.</summary>
    public string ToJson()
    {
        var builder = new StringBuilder();
        Write(builder);
        return builder.ToString();
    }

    /// <inheritdoc />
    public override string ToString() => ToJson();

    /// <summary>Do two values hold the same JSON? Numbers compare as IEEE values, object keys in any order.</summary>
    public static bool DeepEquals(JsonValue? a, JsonValue? b)
    {
        switch (a, b)
        {
            case (null, null):
                return true;
            case (JsonNull, JsonNull):
                return true;
            case (JsonBool x, JsonBool y):
                return x.Value == y.Value;
            case (JsonNumber x, JsonNumber y):
                return x.Value == y.Value;
            case (JsonString x, JsonString y):
                return string.Equals(x.Value, y.Value, StringComparison.Ordinal);
            case (JsonArray x, JsonArray y):
                return x.Count == y.Count && Enumerable.Range(0, x.Count).All(i => DeepEquals(x[i], y[i]));
            case (JsonObject x, JsonObject y):
                return x.Count == y.Count && x.Properties.All(pair => y.TryGetValue(pair.Key, out var other) && DeepEquals(pair.Value, other));
            default:
                return false;
        }
    }

    internal abstract void Write(StringBuilder builder);

    internal static void WriteString(StringBuilder builder, string text)
    {
        builder.Append('"');
        for (var i = 0; i < text.Length; i++)
        {
            var c = text[i];
            switch (c)
            {
                case '"':
                    builder.Append("\\\"");
                    break;
                case '\\':
                    builder.Append("\\\\");
                    break;
                case '\n':
                    builder.Append("\\n");
                    break;
                case '\r':
                    builder.Append("\\r");
                    break;
                case '\t':
                    builder.Append("\\t");
                    break;
                case '\b':
                    builder.Append("\\b");
                    break;
                case '\f':
                    builder.Append("\\f");
                    break;
                default:
                    if (c < 0x20 || (char.IsSurrogate(c) && !(char.IsHighSurrogate(c) && i + 1 < text.Length && char.IsLowSurrogate(text[i + 1])) && !(char.IsLowSurrogate(c) && i > 0 && char.IsHighSurrogate(text[i - 1]))))
                    {
                        // A control character, or a surrogate with no partner: only an escape can carry it.
                        builder.Append("\\u").Append(((int)c).ToString("x4", CultureInfo.InvariantCulture));
                    }
                    else
                    {
                        builder.Append(c);
                    }

                    break;
            }
        }

        builder.Append('"');
    }
}

/// <summary>The JSON <c>null</c>.</summary>
public sealed class JsonNull : JsonValue
{
    internal JsonNull()
    {
    }

    internal override void Write(StringBuilder builder) => builder.Append("null");
}

/// <summary>A JSON boolean.</summary>
public sealed class JsonBool : JsonValue
{
    internal JsonBool(bool value) => Value = value;

    /// <summary>The boolean.</summary>
    public bool Value { get; }

    internal override void Write(StringBuilder builder) => builder.Append(Value ? "true" : "false");
}

/// <summary>A JSON number, as a binary64 value.</summary>
public sealed class JsonNumber : JsonValue
{
    /// <summary>Makes a number.</summary>
    public JsonNumber(double value) => Value = value;

    /// <summary>The number.</summary>
    public double Value { get; }

    internal override void Write(StringBuilder builder)
    {
        if (!double.IsFinite(Value))
        {
            builder.Append("null");
        }
        else
        {
            builder.Append(Value.ToString("R", CultureInfo.InvariantCulture));
        }
    }
}

/// <summary>A JSON string.</summary>
public sealed class JsonString : JsonValue
{
    /// <summary>Makes a string.</summary>
    public JsonString(string value) => Value = value ?? throw new ArgumentNullException(nameof(value));

    /// <summary>The string, as UTF-16 code units.</summary>
    public string Value { get; }

    internal override void Write(StringBuilder builder) => WriteString(builder, Value);
}

/// <summary>A JSON array.</summary>
public sealed class JsonArray : JsonValue
{
    private readonly JsonValue[] _items;

    /// <summary>Makes an array.</summary>
    public JsonArray(IEnumerable<JsonValue> items) => _items = [.. items];

    /// <summary>How many elements.</summary>
    public int Count => _items.Length;

    /// <summary>The elements.</summary>
    public IReadOnlyList<JsonValue> Items => _items;

    /// <summary>The element at <paramref name="index"/>.</summary>
    public JsonValue this[int index] => _items[index];

    internal override void Write(StringBuilder builder)
    {
        builder.Append('[');
        for (var i = 0; i < _items.Length; i++)
        {
            if (i > 0)
            {
                builder.Append(',');
            }

            _items[i].Write(builder);
        }

        builder.Append(']');
    }
}

/// <summary>A JSON object. A duplicate key in the text keeps the last value.</summary>
public sealed class JsonObject : JsonValue
{
    private readonly Dictionary<string, JsonValue> _properties;

    /// <summary>Makes an object; a later pair replaces an earlier one with the same key.</summary>
    public JsonObject(IEnumerable<KeyValuePair<string, JsonValue>> properties)
    {
        _properties = new Dictionary<string, JsonValue>(StringComparer.Ordinal);
        foreach (var (key, value) in properties)
        {
            _properties[key] = value;
        }
    }

    /// <summary>How many properties.</summary>
    public int Count => _properties.Count;

    /// <summary>The properties.</summary>
    public IReadOnlyDictionary<string, JsonValue> Properties => _properties;

    /// <summary>The property named <paramref name="name"/>, if there is one (a JSON null counts as present).</summary>
    public bool TryGetValue(string name, out JsonValue value) => _properties.TryGetValue(name, out value!);

    internal override void Write(StringBuilder builder)
    {
        builder.Append('{');
        var first = true;
        foreach (var (key, value) in _properties)
        {
            if (!first)
            {
                builder.Append(',');
            }

            first = false;
            WriteString(builder, key);
            builder.Append(':');
            value.Write(builder);
        }

        builder.Append('}');
    }
}
