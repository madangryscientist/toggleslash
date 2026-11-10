"""An incremental Server-Sent Events parser, as the WHATWG standard reads a stream."""

from __future__ import annotations

import codecs
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class SseEvent:
    event: str
    data: str


class SseParser:
    """Feed it bytes as they arrive, in any chunking; it returns the events completed so far.

    A line ends with LF, CRLF or a lone CR (a CR at the end of a chunk waits to see whether an LF
    follows). A line that starts with ``:`` is a comment. Only ``event`` and ``data`` are read: several
    ``data`` lines are joined with LF, an event with no ``data`` line is dropped, and the name is
    ``message`` when none was given. A leading byte order mark is dropped, and UTF-8 may be split across
    chunks. An event still incomplete when the stream ends is never returned.
    """

    def __init__(self) -> None:
        self._decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self._line: list[str] = []
        self._after_cr = False
        self._started = False
        self._event = ""
        self._data: list[str] = []

    def feed(self, chunk: bytes) -> list[SseEvent]:
        text = self._decoder.decode(chunk)
        if text and not self._started:
            self._started = True
            text = text.removeprefix("﻿")
        events: list[SseEvent] = []
        for char in text:
            if self._after_cr:
                self._after_cr = False
                if char == "\n":
                    continue
            if char == "\r":
                self._after_cr = True
                self._end_line(events)
            elif char == "\n":
                self._end_line(events)
            else:
                self._line.append(char)
        return events

    def _end_line(self, events: list[SseEvent]) -> None:
        line = "".join(self._line)
        self._line.clear()
        if line == "":
            if self._data:
                events.append(SseEvent(self._event or "message", "\n".join(self._data)))
            self._event = ""
            self._data = []
            return
        if line.startswith(":"):
            return
        field, colon, value = line.partition(":")
        if colon:
            value = value.removeprefix(" ")
        if field == "event":
            self._event = value
        elif field == "data":
            self._data.append(value)
