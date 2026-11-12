package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Utf8;
import java.util.ArrayList;
import java.util.List;

/**
 * Server-Sent Events as the WHATWG standard reads them, fed bytes. It decodes UTF-8 incrementally
 * (a character split across reads is kept), ends a line on LF, CRLF or a lone CR (an LF that
 * follows a CR, even in the next read, belongs to it), skips comments and a leading byte order
 * mark, and reads only {@code event} and {@code data}. Several {@code data} lines of one event are
 * joined with LF, an event with no {@code data} line is dropped, an event with no name is {@code
 * message}, and an event still incomplete when the stream ends is never dispatched.
 */
final class SseParser {
  /** One dispatched event. */
  record Event(String name, String data) {}

  private final Utf8.Decoder decoder = new Utf8.Decoder(true);
  private final StringBuilder decoded = new StringBuilder();
  private final StringBuilder line = new StringBuilder();
  private final StringBuilder data = new StringBuilder();
  private String eventName = "";
  private boolean skipLf;

  /**
   * Feeds bytes.
   *
   * @param bytes the bytes read
   * @return the events they completed, in order
   */
  List<Event> feed(byte[] bytes) {
    decoded.setLength(0);
    decoder.feed(bytes, 0, bytes.length, decoded);
    List<Event> events = new ArrayList<>();
    for (int i = 0; i < decoded.length(); i++) {
      char c = decoded.charAt(i);
      if (c == '\n') {
        if (skipLf) {
          skipLf = false;
        } else {
          endLine(events);
        }
      } else if (c == '\r') {
        skipLf = true;
        endLine(events);
      } else {
        skipLf = false;
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
      eventName = value;
    } else if (field.equals("data")) {
      data.append(value).append('\n');
    }
  }

  private void dispatch(List<Event> events) {
    if (data.length() > 0) {
      data.setLength(data.length() - 1);
      events.add(new Event(eventName.isEmpty() ? "message" : eventName, data.toString()));
    }
    data.setLength(0);
    eventName = "";
  }
}
