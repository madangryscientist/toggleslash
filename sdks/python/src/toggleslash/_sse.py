"""A server-sent events parser, to the WHATWG rules: UTF-8 decoded incrementally, lines end on LF, CRLF or a
lone CR, `:` starts a comment, one leading space of a value is dropped, and an event with no data is dropped."""

from __future__ import annotations

import codecs
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class SseEvent:
    name: str
    data: str


class SseParser:
    """Feed it bytes as they arrive; it returns the events they completed. An event still incomplete when the
    stream ends is never returned."""

    def __init__(self) -> None:
        self._decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self._buffer = ""
        self._started = False
        self._skip_lf = False
        self._name = ""
        self._data: list[str] = []

    def feed(self, data: bytes) -> list[SseEvent]:
        text = self._decoder.decode(data)
        if text and not self._started:
            self._started = True
            text = text.removeprefix("﻿")
        buffer = self._buffer + text
        if self._skip_lf and buffer:
            # The previous read ended on a CR, which already ended its line: an LF now is the same line end.
            self._skip_lf = False
            buffer = buffer.removeprefix("\n")
        events: list[SseEvent] = []
        size = len(buffer)
        start = 0
        while start < size:
            end = start
            while end < size and buffer[end] not in "\r\n":
                end += 1
            if end == size:
                break
            if buffer[end] == "\r":
                if end + 1 == size:
                    self._skip_lf = True
                    after = end + 1
                else:
                    after = end + 2 if buffer[end + 1] == "\n" else end + 1
            else:
                after = end + 1
            self._line(buffer[start:end], events)
            start = after
        self._buffer = buffer[start:]
        return events

    def _line(self, line: str, events: list[SseEvent]) -> None:
        if not line:
            if self._data:
                events.append(SseEvent(self._name or "message", "\n".join(self._data)))
            self._name = ""
            self._data = []
            return
        if line.startswith(":"):
            return
        field, colon, value = line.partition(":")
        if colon:
            value = value.removeprefix(" ")
        if field == "event":
            self._name = value
        elif field == "data":
            self._data.append(value)
