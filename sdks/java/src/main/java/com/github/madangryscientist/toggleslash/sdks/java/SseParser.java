package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.List;

/**
 * An incremental Server-Sent Events parser to the WHATWG rules, fed text in any chunking. Only the
 * {@code event} and {@code data} fields are read; a comment, any other field, an event with no data
 * and an event left unfinished at the end are dropped.
 */
final class SseParser {
  /** One event. */
  record Event(String name, String data) {}

  private String buffer = "";

  List<Event> feed(String text) {
    String joined = buffer + text;
    // A CR at the end may be half of a CRLF split across chunks: wait for the next one.
    boolean held = joined.endsWith("\r");
    String body = held ? joined.substring(0, joined.length() - 1) : joined;
    body = body.replace("\r\n", "\n").replace('\r', '\n');
    List<Event> events = new ArrayList<>();
    int start = 0;
    while (true) {
      int end = body.indexOf("\n\n", start);
      if (end < 0) {
        break;
      }
      Event event = parseBlock(body.substring(start, end));
      if (event != null) {
        events.add(event);
      }
      start = end + 2;
    }
    buffer = body.substring(start) + (held ? "\r" : "");
    return events;
  }

  private static Event parseBlock(String block) {
    String name = "message";
    List<String> data = new ArrayList<>();
    int pos = 0;
    while (pos <= block.length()) {
      int eol = block.indexOf('\n', pos);
      String line = block.substring(pos, eol < 0 ? block.length() : eol);
      pos = eol < 0 ? block.length() + 1 : eol + 1;
      if (line.isEmpty() || line.charAt(0) == ':') {
        continue;
      }
      int colon = line.indexOf(':');
      String field = colon < 0 ? line : line.substring(0, colon);
      String value = colon < 0 ? "" : line.substring(colon + 1);
      if (value.startsWith(" ")) {
        value = value.substring(1);
      }
      if (field.equals("event")) {
        name = value;
      } else if (field.equals("data")) {
        data.add(value);
      }
    }
    return data.isEmpty() ? null : new Event(name, String.join("\n", data));
  }
}
