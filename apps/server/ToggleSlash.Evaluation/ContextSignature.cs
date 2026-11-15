using System.Globalization;
using System.Text;
using System.Text.Json;

namespace ToggleSlash.Evaluation;

/// <summary>
/// The bytes a secure-mode signature covers, to "Secure mode" in docs/sdk-browser.md: the context normalised as
/// docs/evaluation.md does it, keys only, the kinds in ordinal order, each kind and then its key written as a
/// netstring (<c>&lt;byte length of the UTF-8&gt;:&lt;UTF-8 bytes&gt;,</c>). Attributes are not part of it.
/// </summary>
public static class ContextSignature
{
    /// <summary>The message for <paramref name="context"/>, or null when the context is not valid.</summary>
    public static byte[]? Message(JsonElement context)
    {
        if (ContextKinds.Normalise(context) is not { } contexts) return null;
        var message = new List<byte>();
        foreach (var kind in contexts.Keys.Order(StringComparer.Ordinal))
        {
            Netstring(message, kind);
            Netstring(message, contexts[kind].Key);
        }
        return [.. message];
    }

    private static void Netstring(List<byte> into, string text)
    {
        var bytes = Encoding.UTF8.GetBytes(text);
        into.AddRange(Encoding.ASCII.GetBytes(bytes.Length.ToString(CultureInfo.InvariantCulture)));
        into.Add((byte)':');
        into.AddRange(bytes);
        into.Add((byte)',');
    }
}
