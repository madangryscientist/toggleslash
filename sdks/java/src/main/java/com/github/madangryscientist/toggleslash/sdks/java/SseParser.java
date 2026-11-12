package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.List;

/**
 * An incremental Server-Sent Events parser to the WHATWG rules, over text already decoded. Feed it
 * text in any chunking. A line ends at LF, CRLF or a lone CR (a CR at the end of a chunk waits for
 * a possible LF). A line starting with a colon is a comment. Only {@code event} and {@code data}
 * are read. An event with no data line is dropped, and the name defaults to {@code message}. An
 * event incomplete at the end is never returned.
 */
final class SseParser {
  /** One event: its name and its data. */
  record Event(String name, String data) {}

  private final StringBuilder line = new StringBuilder();
  private boolean afterCr;
  private String name = "";
  private final StringBuilder data = new StringBuilder();
  private boolean hasData;

  /** Takes the next piece of text and returns the events it completed. */
  List<Event> feed(CharSequence text) {
    List<Event> events = new ArrayList<>();
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
      if (afterCr) {
        afterCr = false;
        if (c == '\n') {
          continue;
        }
      }
      if (c == '\r') {
        afterCr = true;
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
      dispatch(events);
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

  private void dispatch(List<Event> events) {
    if (hasData) {
      events.add(new Event(name.isEmpty() ? "message" : name, data.toString()));
    }
    name = "";
    data.setLength(0);
    hasData = false;
  }
}
