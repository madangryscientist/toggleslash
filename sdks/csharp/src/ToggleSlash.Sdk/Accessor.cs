using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk;

internal enum JsonKind
{
    Null,
    Bool,
    Number,
    String,
    Array,
    Object,
}

/// <summary>What a static type <c>T</c> reads from a served value (contract section 4).</summary>
internal enum AccessorKind
{
    /// <summary>A type with no JSON kind: it matches no served value.</summary>
    None,
    Bool,
    String,
    Number,
    Integer,
    Json,
}

internal static class Kinds
{
    internal static JsonKind Of(JsonValue value) => value switch
    {
        JsonBool => JsonKind.Bool,
        JsonNumber => JsonKind.Number,
        JsonString => JsonKind.String,
        JsonArray => JsonKind.Array,
        JsonObject => JsonKind.Object,
        _ => JsonKind.Null,
    };
}

/// <summary>
/// The mismatch rule for the static type <typeparamref name="T"/>. <c>bool</c>, <c>string</c>, the integer types, the
/// floating types, <see cref="JsonValue"/> and the <c>Nullable&lt;&gt;</c> of a value type are read; any other type has
/// no JSON kind, so the caller gets the fallback with <c>TYPE_MISMATCH</c>.
/// </summary>
internal static class Accessor<T>
{
    private static readonly Type Underlying = Nullable.GetUnderlyingType(typeof(T)) ?? typeof(T);
    private static readonly AccessorKind Kind = KindOf(Underlying);
    private static readonly double Minimum = IntegerMinimum(Underlying);
    private static readonly double MaximumExclusive = IntegerMaximumExclusive(Underlying);

    /// <summary>Reads a served (non-fallback) value as <typeparamref name="T"/>; false is a type mismatch.</summary>
    internal static bool TryConvert(JsonValue served, T fallback, out T result)
    {
        result = fallback;
        switch (Kind)
        {
            case AccessorKind.Bool:
                if (served is JsonBool boolean)
                {
                    result = (T)(object)boolean.Value;
                    return true;
                }

                return false;
            case AccessorKind.String:
                if (served is JsonString text)
                {
                    result = (T)(object)text.Value;
                    return true;
                }

                return false;
            case AccessorKind.Number:
                if (served is JsonNumber number)
                {
                    result = Underlying == typeof(float) ? (T)(object)(float)number.Value : (T)(object)number.Value;
                    return true;
                }

                return false;
            case AccessorKind.Integer:
                if (served is JsonNumber whole && whole.Value >= Minimum && whole.Value < MaximumExclusive && whole.Value == Math.Floor(whole.Value))
                {
                    result = (T)IntegerFrom(whole.Value);
                    return true;
                }

                return false;
            case AccessorKind.Json:
                // A null (or JSON null) fallback says nothing about the kind wanted, so it accepts any served value;
                // a non-null one compares kinds, and a served null never matches it.
                if (fallback is null || fallback is JsonNull || Kinds.Of((JsonValue)(object)fallback) == Kinds.Of(served))
                {
                    result = (T)(object)served;
                    return true;
                }

                return false;
            default:
                return false;
        }
    }

    // The value is whole and inside the type's range, so ChangeType converts it exactly.
    private static object IntegerFrom(double value) =>
        System.Convert.ChangeType(value, Underlying, System.Globalization.CultureInfo.InvariantCulture);

    private static AccessorKind KindOf(Type type)
    {
        if (type == typeof(bool))
        {
            return AccessorKind.Bool;
        }

        if (type == typeof(string))
        {
            return AccessorKind.String;
        }

        if (type == typeof(double) || type == typeof(float))
        {
            return AccessorKind.Number;
        }

        if (type == typeof(long) || type == typeof(ulong) || type == typeof(int) || type == typeof(uint)
            || type == typeof(short) || type == typeof(ushort) || type == typeof(sbyte) || type == typeof(byte))
        {
            return AccessorKind.Integer;
        }

        return type == typeof(JsonValue) ? AccessorKind.Json : AccessorKind.None;
    }

    private static double IntegerMinimum(Type type)
    {
        if (type == typeof(long))
        {
            return long.MinValue;
        }

        if (type == typeof(int))
        {
            return int.MinValue;
        }

        if (type == typeof(short))
        {
            return short.MinValue;
        }

        return type == typeof(sbyte) ? sbyte.MinValue : 0;
    }

    private static double IntegerMaximumExclusive(Type type)
    {
        // (double)MaxValue + 1 is exact for every type here, including 2^63 and 2^64.
        if (type == typeof(long))
        {
            return (double)long.MaxValue + 1;
        }

        if (type == typeof(ulong))
        {
            return (double)ulong.MaxValue + 1;
        }

        if (type == typeof(int))
        {
            return (double)int.MaxValue + 1;
        }

        if (type == typeof(uint))
        {
            return (double)uint.MaxValue + 1;
        }

        if (type == typeof(short))
        {
            return (double)short.MaxValue + 1;
        }

        if (type == typeof(ushort))
        {
            return (double)ushort.MaxValue + 1;
        }

        if (type == typeof(sbyte))
        {
            return (double)sbyte.MaxValue + 1;
        }

        return (double)byte.MaxValue + 1;
    }
}
