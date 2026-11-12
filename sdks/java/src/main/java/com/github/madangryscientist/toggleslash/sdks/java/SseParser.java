package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * A Server-Sent Events parser, as the WHATWG standard reads a stream: LF, CRLF and a lone CR end a
 * line, a line starting with a colon is a comment, and only {@code event} and {@code data} are
 * read. An event still incomplete when the stream ends is never dispatched.
 */
final class SseParser {
  /** Receives each complete event. */
  interface Sink {
    /** Returns false to stop reading the stream. */
    boolean event(String name, String data);
  }

  private final Sink sink;
  private final StringBuilder line = new StringBuilder();
  private final StringBuilder data = new StringBuilder();
  private String eventName = "";
  private boolean hasData;
  private boolean afterCr;
  private boolean atStart = true;
  private boolean stopped;

  SseParser(Sink sink) {
    this.sink = sink;
  }

  /** Feeds decoded text; false once the sink asked to stop. */
  boolean accept(CharSequence text) {
    for (int i = 0; i < text.length() && !stopped; i++) {
      char c = text.charAt(i);
      if (atStart) {
        atStart = false;
        if (c == '﻿') {
          continue;
        }
      }
      if (afterCr) {
        afterCr = false;
        if (c == '\n') {
          continue;
        }
      }
      if (c == '\r') {
        endLine();
        afterCr = true;
      } else if (c == '\n') {
        endLine();
      } else {
        line.append(c);
      }
    }
    return !stopped;
  }

  private void endLine() {
    if (line.length() == 0) {
      dispatch();
      return;
    }
    if (line.charAt(0) != ':') {
      int colon = line.indexOf(":");
      String name;
      String value;
      if (colon < 0) {
        name = line.toString();
        value = "";
      } else {
        name = line.substring(0, colon);
        int start = colon + 1;
        if (start < line.length() && line.charAt(start) == ' ') {
          start++;
        }
        value = line.substring(start);
      }
      if (name.equals("event")) {
        eventName = value;
      } else if (name.equals("data")) {
        data.append(value).append('\n');
        hasData = true;
      }
    }
    line.setLength(0);
  }

  private void dispatch() {
    line.setLength(0);
    String name = eventName.isEmpty() ? "message" : eventName;
    boolean had = hasData;
    String payload = had ? data.substring(0, data.length() - 1) : "";
    eventName = "";
    data.setLength(0);
    hasData = false;
    if (had && !sink.event(name, payload)) {
      stopped = true;
    }
  }
}
