using System.Text;

namespace ToggleSlash.Sdk.Evaluation;

/// <summary>
/// An attribute reference, to "Reading the attribute" in docs/evaluation.md: a name that starts with <c>/</c>. The
/// components are what is left after dropping the leading <c>/</c>, splitting on <c>/</c> and decoding <c>~1</c> as
/// <c>/</c> and <c>~0</c> as <c>~</c> in one left-to-right pass.
/// </summary>
internal static class AttributeReference
{
    /// <summary>
    /// The components of <paramref name="text"/> (a name starting with <c>/</c>), or null when a component is empty or
    /// a <c>~</c> is followed by anything but <c>0</c> or <c>1</c>, or ends the component.
    /// </summary>
    public static IReadOnlyList<string>? Parse(string text)
    {
        var components = new List<string>();
        foreach (var part in text[1..].Split('/'))
        {
            if (part.Length == 0)
            {
                return null;
            }

            var decoded = new StringBuilder();
            for (var i = 0; i < part.Length; i++)
            {
                if (part[i] != '~')
                {
                    decoded.Append(part[i]);
                    continue;
                }

                if (i + 1 >= part.Length || part[i + 1] is not ('0' or '1'))
                {
                    return null;
                }

                decoded.Append(part[i + 1] == '0' ? '~' : '/');
                i++;
            }

            components.Add(decoded.ToString());
        }

        return components;
    }
}
