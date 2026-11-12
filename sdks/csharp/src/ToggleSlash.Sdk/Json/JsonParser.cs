using System.Globalization;

namespace ToggleSlash.Sdk.Json;

/// <summary>
/// A recursive-descent RFC 8259 parser over a string. A byte order mark at the start is skipped, a duplicate object key
/// keeps the last value, a number is read as binary64 (an overflow is Infinity), and an escaped lone surrogate is kept.
/// Nesting past <see cref="MaxDepth"/> levels is refused, which the contract allows past 512.
/// </summary>
internal sealed class JsonParser
{
    internal const int MaxDepth = 512;

    private readonly string _text;
    private int _position;

    private JsonParser(string text) => _text = text;

    /// <summary>The value <paramref name="text"/> holds, or null when it is not valid JSON.</summary>
    internal static JsonValue? Parse(string text)
    {
        var parser = new JsonParser(text);
        if (parser._position < text.Length && text[0] == '﻿')
        {
            parser._position = 1;
        }

        parser.SkipWhitespace();
        var value = parser.ReadValue(0);
        if (value is null)
        {
            return null;
        }

        parser.SkipWhitespace();
        return parser._position == text.Length ? value : null;
    }

    private void SkipWhitespace()
    {
        while (_position < _text.Length && _text[_position] is ' ' or '\t' or '\n' or '\r')
        {
            _position++;
        }
    }

    private JsonValue? ReadValue(int depth)
    {
        if (_position >= _text.Length)
        {
            return null;
        }

        switch (_text[_position])
        {
            case '{':
                return ReadObject(depth + 1);
            case '[':
                return ReadArray(depth + 1);
            case '"':
                return ReadString() is { } text ? new JsonString(text) : null;
            case 't':
                return ReadWord("true", JsonValue.True);
            case 'f':
                return ReadWord("false", JsonValue.False);
            case 'n':
                return ReadWord("null", JsonValue.Null);
            default:
                return ReadNumber();
        }
    }

    private JsonValue? ReadWord(string word, JsonValue value)
    {
        if (string.CompareOrdinal(_text, _position, word, 0, word.Length) != 0)
        {
            return null;
        }

        _position += word.Length;
        return value;
    }

    private static bool IsDigit(char c) => c is >= '0' and <= '9';

    private JsonNumber? ReadNumber()
    {
        var start = _position;
        if (_position < _text.Length && _text[_position] == '-')
        {
            _position++;
        }

        if (_position >= _text.Length || !IsDigit(_text[_position]))
        {
            return null;
        }

        if (_text[_position] == '0')
        {
            _position++;
        }
        else
        {
            while (_position < _text.Length && IsDigit(_text[_position]))
            {
                _position++;
            }
        }

        if (_position < _text.Length && _text[_position] == '.')
        {
            _position++;
            if (!SkipDigits())
            {
                return null;
            }
        }

        if (_position < _text.Length && _text[_position] is 'e' or 'E')
        {
            _position++;
            if (_position < _text.Length && _text[_position] is '+' or '-')
            {
                _position++;
            }

            if (!SkipDigits())
            {
                return null;
            }
        }

        // .NET Core 3.0 and later return Infinity for an overflow rather than throwing.
        var number = double.Parse(_text.AsSpan(start, _position - start), NumberStyles.Float, CultureInfo.InvariantCulture);
        return new JsonNumber(number);
    }

    private bool SkipDigits()
    {
        var start = _position;
        while (_position < _text.Length && IsDigit(_text[_position]))
        {
            _position++;
        }

        return _position > start;
    }

    private static int HexValue(char c) => c switch
    {
        >= '0' and <= '9' => c - '0',
        >= 'a' and <= 'f' => c - 'a' + 10,
        >= 'A' and <= 'F' => c - 'A' + 10,
        _ => -1,
    };

    private string? ReadString()
    {
        _position++; // the opening quote
        var builder = new System.Text.StringBuilder();
        while (_position < _text.Length)
        {
            var c = _text[_position++];
            if (c == '"')
            {
                return builder.ToString();
            }

            if (c < 0x20)
            {
                return null;
            }

            if (c != '\\')
            {
                builder.Append(c);
                continue;
            }

            if (_position >= _text.Length)
            {
                return null;
            }

            var escape = _text[_position++];
            switch (escape)
            {
                case '"' or '\\' or '/':
                    builder.Append(escape);
                    break;
                case 'b':
                    builder.Append('\b');
                    break;
                case 'f':
                    builder.Append('\f');
                    break;
                case 'n':
                    builder.Append('\n');
                    break;
                case 'r':
                    builder.Append('\r');
                    break;
                case 't':
                    builder.Append('\t');
                    break;
                case 'u':
                    if (_position + 4 > _text.Length)
                    {
                        return null;
                    }

                    var code = 0;
                    for (var i = 0; i < 4; i++)
                    {
                        var digit = HexValue(_text[_position + i]);
                        if (digit < 0)
                        {
                            return null;
                        }

                        code = code * 16 + digit;
                    }

                    _position += 4;
                    builder.Append((char)code);
                    break;
                default:
                    return null;
            }
        }

        return null;
    }

    private JsonArray? ReadArray(int depth)
    {
        if (depth > MaxDepth)
        {
            return null;
        }

        _position++; // [
        var items = new List<JsonValue>();
        SkipWhitespace();
        if (_position < _text.Length && _text[_position] == ']')
        {
            _position++;
            return new JsonArray(items);
        }

        while (true)
        {
            SkipWhitespace();
            if (ReadValue(depth) is not { } item)
            {
                return null;
            }

            items.Add(item);
            SkipWhitespace();
            if (_position >= _text.Length)
            {
                return null;
            }

            var c = _text[_position++];
            if (c == ']')
            {
                return new JsonArray(items);
            }

            if (c != ',')
            {
                return null;
            }
        }
    }

    private JsonObject? ReadObject(int depth)
    {
        if (depth > MaxDepth)
        {
            return null;
        }

        _position++; // {
        var properties = new List<KeyValuePair<string, JsonValue>>();
        SkipWhitespace();
        if (_position < _text.Length && _text[_position] == '}')
        {
            _position++;
            return new JsonObject(properties);
        }

        while (true)
        {
            SkipWhitespace();
            if (_position >= _text.Length || _text[_position] != '"' || ReadString() is not { } name)
            {
                return null;
            }

            SkipWhitespace();
            if (_position >= _text.Length || _text[_position++] != ':')
            {
                return null;
            }

            SkipWhitespace();
            if (ReadValue(depth) is not { } value)
            {
                return null;
            }

            properties.Add(new KeyValuePair<string, JsonValue>(name, value));
            SkipWhitespace();
            if (_position >= _text.Length)
            {
                return null;
            }

            var c = _text[_position++];
            if (c == '}')
            {
                return new JsonObject(properties);
            }

            if (c != ',')
            {
                return null;
            }
        }
    }
}
