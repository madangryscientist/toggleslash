using System.Text;

namespace ToggleSlash.Sdk.Sse;

/// <summary>One dispatched Server-Sent Event.</summary>
internal readonly record struct SseEvent(string Name, string Data);

/// <summary>
/// A WHATWG-style Server-Sent Events parser over decoded text, in any chunking. LF, CRLF and a lone CR end a line (a CR at
/// the end of a chunk is remembered, so a CRLF split across chunks is one line end); a line starting with <c>:</c> is a
/// comment; only <c>event</c> and <c>data</c> are read; several <c>data</c> lines are joined with LF; an event with no
/// <c>data</c> line is dropped; the name is <c>message</c> when none was given; an event incomplete at the end is discarded
/// (nothing here dispatches on the end). A leading U+FEFF is skipped.
/// </summary>
internal sealed class SseParser
{
    private readonly StringBuilder _line = new();
    private readonly StringBuilder _data = new();
    private string? _name;
    private bool _hasData;
    private bool _lastWasCarriageReturn;
    private bool _atStart = true;

    /// <summary>Feeds a chunk of decoded text and appends each event it completes to <paramref name="events"/>.</summary>
    internal void Feed(ReadOnlySpan<char> chunk, List<SseEvent> events)
    {
        foreach (var c in chunk)
        {
            if (_atStart)
            {
                _atStart = false;
                if (c == '﻿')
                {
                    continue;
                }
            }

            if (_lastWasCarriageReturn)
            {
                _lastWasCarriageReturn = false;
                if (c == '\n')
                {
                    continue;
                }
            }

            if (c == '\r')
            {
                EndLine(events);
                _lastWasCarriageReturn = true;
            }
            else if (c == '\n')
            {
                EndLine(events);
            }
            else
            {
                _line.Append(c);
            }
        }
    }

    private void EndLine(List<SseEvent> events)
    {
        var line = _line.ToString();
        _line.Clear();
        if (line.Length == 0)
        {
            Dispatch(events);
            return;
        }

        if (line[0] == ':')
        {
            return;
        }

        string field;
        string value;
        var colon = line.IndexOf(':', StringComparison.Ordinal);
        if (colon < 0)
        {
            field = line;
            value = string.Empty;
        }
        else
        {
            field = line[..colon];
            value = line[(colon + 1)..];
            if (value.StartsWith(' '))
            {
                value = value[1..];
            }
        }

        if (field == "event")
        {
            _name = value;
        }
        else if (field == "data")
        {
            _data.Append(value).Append('\n');
            _hasData = true;
        }
    }

    private void Dispatch(List<SseEvent> events)
    {
        if (_hasData)
        {
            if (_data.Length > 0)
            {
                _data.Length--;
            }

            events.Add(new SseEvent(string.IsNullOrEmpty(_name) ? "message" : _name, _data.ToString()));
        }

        _name = null;
        _data.Clear();
        _hasData = false;
    }
}
