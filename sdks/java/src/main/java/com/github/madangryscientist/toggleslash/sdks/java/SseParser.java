package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.List;

/**
 * An incremental Server-Sent Events parser, to the WHATWG rules. A line ends at LF, CRLF or a lone
 * CR (a CRLF split across two pieces of text is one ending), a line starting with a colon is a
 * comment, several {@code data} lines join with LF, an event with no {@code data} line is dropped,
 * and an event name is {@code message} when none is given. An event still incomplete when the
 * stream ends is never emitted.
 */
final class SseParser {
  /** One event: its name and its data. */
  record Event(String name, String data) {}

  private final StringBuilder line = new StringBuilder();
  private final StringBuilder data = new StringBuilder();
  private String name = "";
  private boolean hasData;
  private boolean skipLf;

  /** Feeds decoded text and appends each event it completes to {@code out}. */
  void feed(CharSequence text, List<Event> out) {
    for (int i = 0; i < text.length(); i++) {
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
    String value = "";
    if (colon >= 0) {
      int start = colon + 1;
      if (start < line.length() && line.charAt(start) == ' ') {
        start++;
      }
      value = line.substring(start);
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
