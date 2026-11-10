package com.github.madangryscientist.toggleslash.sdks.java;

import java.nio.ByteBuffer;
import java.nio.CharBuffer;
import java.nio.charset.CharsetDecoder;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

/**
 * A server-sent events parser, to the WHATWG rules: lines end in LF, CRLF or a lone CR; a line that
 * starts with a colon is a comment; the event name defaults to {@code message}; several {@code
 * data} lines are joined with LF; an event with no {@code data} line is dropped; an event still
 * open when the input ends is discarded (it is never flushed). Bytes are decoded as UTF-8 across
 * chunk boundaries, with U+FFFD for bad bytes.
 */
final class SseParser {
  /** One dispatched event. */
  record Event(String name, String data) {}

  private final CharsetDecoder decoder =
      StandardCharsets.UTF_8
          .newDecoder()
          .onMalformedInput(CodingErrorAction.REPLACE)
          .onUnmappableCharacter(CodingErrorAction.REPLACE);
  private ByteBuffer pending = ByteBuffer.allocate(0);
  private final StringBuilder line = new StringBuilder();
  private final StringBuilder data = new StringBuilder();
  private String name = "";
  private boolean hasData;
  private boolean skipLf;
  private boolean atStart = true;

  /** Feeds the next bytes and returns the events they completed. */
  List<Event> feed(byte[] chunk, int length) {
    ByteBuffer input = ByteBuffer.allocate(pending.remaining() + length);
    input.put(pending);
    input.put(chunk, 0, length);
    input.flip();
    CharBuffer out = CharBuffer.allocate(input.remaining() + 1);
    decoder.decode(input, out, false);
    pending = ByteBuffer.allocate(input.remaining());
    pending.put(input);
    pending.flip();
    out.flip();
    List<Event> events = new ArrayList<>();
    while (out.hasRemaining()) {
      char c = out.get();
      if (atStart) {
        atStart = false;
        if (c == '﻿') {
          continue;
        }
      }
      if (skipLf) {
        skipLf = false;
        if (c == '\n') {
          continue;
        }
      }
      if (c == '\r') {
        skipLf = true;
        endLine(events);
      } else if (c == '\n') {
        endLine(events);
      } else {
        line.append(c);
      }
    }
    return events;
  }

  private void endLine(List<Event> events) {
    String text = line.toString();
    line.setLength(0);
    if (text.isEmpty()) {
      if (hasData) {
        events.add(new Event(name.isEmpty() ? "message" : name, data.toString()));
      }
      name = "";
      data.setLength(0);
      hasData = false;
      return;
    }
    if (text.charAt(0) == ':') {
      return;
    }
    int colon = text.indexOf(':');
    String field = colon < 0 ? text : text.substring(0, colon);
    String value = colon < 0 ? "" : text.substring(colon + 1);
    if (value.startsWith(" ")) {
      value = value.substring(1);
    }
    if (field.equals("event")) {
      name = value;
    } else if (field.equals("data")) {
      if (hasData) {
        data.append('\n');
      }
      data.append(value);
      hasData = true;
    }
  }
}
