using System.Text;
using System.Text.Json;

namespace ToggleSlash.Evaluation;

internal static class JsonStrings
{
    /// <summary>
    /// The string a JSON string value spells. <c>JsonElement.GetString()</c> throws on an escaped lone
    /// surrogate (<c>"\ud800"</c>), which is valid JSON text to the parser; the evaluator must not throw,
    /// and the TypeScript engine keeps such a surrogate as it is, so this does too.
    /// </summary>
    public static string Read(JsonElement value)
    {
        try
        {
            return value.GetString()!;
        }
        catch (InvalidOperationException)
        {
            return Unescape(value.GetRawText());
        }
    }

    /// <summary>Unescapes a raw JSON string literal, quotes included. The parser has already checked its syntax.</summary>
    private static string Unescape(string literal)
    {
        var text = new StringBuilder(literal.Length);
        for (var i = 1; i < literal.Length - 1; i++)
        {
            var c = literal[i];
            if (c != '\\')
            {
                text.Append(c);
                continue;
            }
            var escape = literal[++i];
            if (escape == 'u')
            {
                text.Append((char)Convert.ToUInt16(literal.AsSpan(i + 1, 4).ToString(), 16));
                i += 4;
                continue;
            }
            text.Append(escape switch
            {
                'b' => '\b',
                'f' => '\f',
                'n' => '\n',
                'r' => '\r',
                't' => '\t',
                _ => escape, // " \ /
            });
        }
        return text.ToString();
    }
}
