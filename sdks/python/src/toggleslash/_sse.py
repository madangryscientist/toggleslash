"""A Server-Sent Events reader to the WHATWG rules the contract names (7.2, "Reading events")."""

from __future__ import annotations

import codecs


class SseParser:
    """Feed it the bytes as they arrive; it returns the events that were completed.

    A line ends at LF, CRLF or a lone CR; UTF-8 may be split across reads; a line that starts with ``:``
    is a comment; only ``event`` and ``data`` are read; an event with no ``data`` line is dropped; an
    event still incomplete when the stream ends is discarded (the caller just stops feeding).
    """

    def __init__(self) -> None:
        self._decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self._tail = ""
        self._skip_lf = False
        self._name = ""
        self._data: list[str] = []

    def feed(self, chunk: bytes) -> list[tuple[str, str]]:
        text = self._decoder.decode(chunk)
        if self._skip_lf and text:
            if text.startswith("\n"):
                text = text[1:]
            self._skip_lf = False
        text = self._tail + text
        events: list[tuple[str, str]] = []
        start = 0
        n = len(text)
        i = 0
        while i < n:
            ch = text[i]
            if ch in "\r\n":
                event = self._line(text[start:i])
                if event is not None:
                    events.append(event)
                if ch == "\r":
                    if i + 1 < n:
                        if text[i + 1] == "\n":
                            i += 1
                    else:
                        self._skip_lf = True
                i += 1
                start = i
            else:
                i += 1
        self._tail = text[start:]
        return events

    def _line(self, line: str) -> tuple[str, str] | None:
        if line == "":
            return self._dispatch()
        if line.startswith(":"):
            return None
        name, colon, value = line.partition(":")
        if colon and value.startswith(" "):
            value = value[1:]
        if name == "event":
            self._name = value
        elif name == "data":
            self._data.append(value)
        return None

    def _dispatch(self) -> tuple[str, str] | None:
        data, name = self._data, self._name
        self._data = []
        self._name = ""
        if not data:
            return None
        return (name or "message"), "\n".join(data)
