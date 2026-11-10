"""A Server-Sent Events reader, to the WHATWG rules the contract names (section 7.2)."""

from __future__ import annotations

import codecs


class SseParser:
    """Feed it the bytes of a stream as they arrive; it returns the events they completed.

    An event is `(name, data)`. Only the `event` and `data` fields are read. An event with no `data` line is
    dropped, the name is `message` when none was given, and an event still incomplete when the bytes end is
    never delivered (the caller just stops feeding).
    """

    def __init__(self) -> None:
        self._decoder = codecs.getincrementaldecoder("utf-8")(errors="replace")
        self._line = ""  # the unfinished line
        self._after_cr = False  # the last character read was a CR, so an LF next is the same line end
        self._started = False  # the first character (a BOM is dropped) has been seen
        self._name = ""
        self._data: list[str] = []
        self._has_data = False

    def feed(self, chunk: bytes) -> list[tuple[str, str]]:
        text = self._decoder.decode(chunk)
        if not self._started and text:
            self._started = True
            if text.startswith("﻿"):
                text = text[1:]
        events: list[tuple[str, str]] = []
        start = 0
        for position, character in enumerate(text):
            if character == "\n" and self._after_cr:
                # The LF of a CRLF whose CR ended the line already.
                self._after_cr = False
                start = position + 1
                continue
            self._after_cr = False
            if character in {"\n", "\r"}:
                self._line += text[start:position]
                start = position + 1
                self._after_cr = character == "\r"
                event = self._end_of_line(self._line)
                self._line = ""
                if event is not None:
                    events.append(event)
        self._line += text[start:]
        return events

    def _end_of_line(self, line: str) -> tuple[str, str] | None:
        if line == "":
            return self._dispatch()
        if line.startswith(":"):
            return None
        field, separator, value = line.partition(":")
        if separator and value.startswith(" "):
            value = value[1:]
        if field == "event":
            self._name = value
        elif field == "data":
            self._data.append(value)
            self._has_data = True
        return None

    def _dispatch(self) -> tuple[str, str] | None:
        name, data, has_data = self._name or "message", "\n".join(self._data), self._has_data
        self._name = ""
        self._data = []
        self._has_data = False
        return (name, data) if has_data else None
