package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.List;

/**
 * An incremental Server-Sent Events parser, as the WHATWG standard reads a stream: LF, CRLF and a
 * lone CR end a line, a line that starts with a colon is a comment, and text may arrive in any
 * chunking. Only the {@code event} and {@code data} fields are kept. An event with no {@code data}
 * line is dropped, and one still incomplete when the stream ends is never dispatched.
 */
final class SseParser {
  /** One dispatched event. */
  record Event(String name, String data) {}

  private final StringBuilder line = new StringBuilder();
  private final StringBuilder data = new StringBuilder();
  private String name = "";
  private boolean hasData;
  private boolean skipLf;
  private boolean atStart = true;

  /** Reads more decoded text and adds the events it completes to {@code out}. */
  void feed(CharSequence text, List<Event> out) {
    int from = 0;
    if (atStart && text.length() > 0) {
      atStart = false;
      if (text.charAt(0) == '﻿') {
        from = 1;
      }
    }
    for (int i = from; i < text.length(); i++) {
      char c = text.charAt(i);
      if (skipLf) {
        skipLf = false;
        if (c == '\n') {
          continue;
        }
      }
      if (c == '\r') {
        endLine(out);
        skipLf = true;
      } else if (c == '\n') {
        endLine(out);
      } else {
        line.append(c);
      }
    }
  }

  private void endLine(List<Event> out) {
    if (line.length() == 0) {
      dispatch(out);
      return;
    }
    if (line.charAt(0) != ':') {
      field();
    }
    line.setLength(0);
  }

  private void field() {
    int colon = line.indexOf(":");
    String field = colon < 0 ? line.toString() : line.substring(0, colon);
    String value = colon < 0 ? "" : line.substring(colon + 1);
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

  private void dispatch(List<Event> out) {
    if (hasData) {
      out.add(new Event(name.isEmpty() ? "message" : name, data.toString()));
    }
    name = "";
    data.setLength(0);
    hasData = false;
  }
}
