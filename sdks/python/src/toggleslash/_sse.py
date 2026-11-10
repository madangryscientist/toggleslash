"""A Server-Sent Events parser, as the WHATWG standard reads them.

LF, CRLF and a lone CR end a line (a CR ends it at once and an LF straight after it is dropped, even when the two
arrive in different reads). A line starting with `:` is a comment. Only `event` and `data` are read; several
`data` lines are joined with LF; an event with no `data` line is dropped; the name is `message` when none was
given. An event still open when the stream ends is never delivered, because only a blank line delivers one.
"""

from __future__ import annotations

from typing import NamedTuple


class SseEvent(NamedTuple):
    event: str
    data: str


class SseParser:
    def __init__(self) -> None:
        self._carry = ""
        self._skip_lf = False
        self._started = False
        self._name = ""
        self._data: list[str] = []

    def feed(self, text: str) -> list[SseEvent]:
        events: list[SseEvent] = []
        if not text:
            return events
        if not self._started:
            self._started = True
            if text.startswith("﻿"):
                text = text[1:]
        if self._skip_lf:
            self._skip_lf = False
            if text.startswith("\n"):
                text = text[1:]
        data = self._carry + text
        self._carry = ""
        i, n = 0, len(data)
        while i < n:
            j = i
            while j < n and data[j] != "\n" and data[j] != "\r":
                j += 1
            if j == n:
                self._carry = data[i:]
                break
            line = data[i:j]
            if data[j] == "\r":
                if j + 1 < n:
                    i = j + 2 if data[j + 1] == "\n" else j + 1
                else:
                    i = j + 1
                    self._skip_lf = True
            else:
                i = j + 1
            self._line(line, events)
        return events

    def _line(self, line: str, events: list[SseEvent]) -> None:
        if line == "":
            if self._data:
                events.append(SseEvent(self._name or "message", "\n".join(self._data)))
            self._name = ""
            self._data = []
            return
        if line.startswith(":"):
            return
        field, sep, value = line.partition(":")
        if sep and value.startswith(" "):
            value = value[1:]
        if field == "event":
            self._name = value
        elif field == "data":
            self._data.append(value)
