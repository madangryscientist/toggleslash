using System.Text;

namespace ToggleSlash.Sdk.Sse;

/// <summary>One server-sent event: its name (<c>message</c> when none was given) and its data.</summary>
internal readonly record struct SseEvent(string Name, string Data);

/// <summary>
/// An incremental Server-Sent Events parser (WHATWG): feed it bytes in any chunking and it returns the events
/// completed so far. Lines end at LF, CRLF or a lone CR; a comment line and every field but <c>event</c> and
/// <c>data</c> are ignored; several <c>data</c> lines join with LF; an event with no <c>data</c> line is dropped;
/// one still incomplete when the stream ends is never returned.
/// </summary>
internal sealed class SseParser
{
    private readonly Decoder _decoder = new UTF8Encoding(false, false).GetDecoder();
    private readonly StringBuilder _line = new();
    private readonly List<string> _data = [];
    private string _name = "";
    private bool _afterCr;
    private bool _first = true;

    /// <summary>Feeds a chunk of the stream and returns the events it completed.</summary>
    public List<SseEvent> Feed(ReadOnlySpan<byte> bytes)
    {
        var chars = new char[_decoder.GetCharCount(bytes, false)];
        var count = _decoder.GetChars(bytes, chars, false);
        var events = new List<SseEvent>();
        var start = 0;
        if (_first && count > 0)
        {
            _first = false;
            if (chars[0] == '﻿') start = 1;
        }
        for (var i = start; i < count; i++)
        {
            var c = chars[i];
            if (_afterCr)
            {
                _afterCr = false;
                if (c == '\n') continue;
            }
            if (c == '\r')
            {
                _afterCr = true;
                EndLine(events);
            }
            else if (c == '\n') EndLine(events);
            else _line.Append(c);
        }
        return events;
    }

    private void EndLine(List<SseEvent> events)
    {
        var line = _line.ToString();
        _line.Clear();
        if (line.Length == 0)
        {
            if (_data.Count > 0) events.Add(new SseEvent(_name.Length == 0 ? "message" : _name, string.Join('\n', _data)));
            _data.Clear();
            _name = "";
            return;
        }
        if (line[0] == ':') return;
        var colon = line.IndexOf(':', StringComparison.Ordinal);
        var field = colon < 0 ? line : line[..colon];
        var value = colon < 0 ? "" : line[(colon + 1)..];
        if (value.StartsWith(' ')) value = value[1..];
        if (field == "event") _name = value;
        else if (field == "data") _data.Add(value);
    }
}
