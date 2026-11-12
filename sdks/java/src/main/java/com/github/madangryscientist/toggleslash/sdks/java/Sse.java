package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.ByteArrayOutputStream;
import java.util.function.Consumer;

/**
 * A Server-Sent Events parser as the WHATWG standard reads them, over bytes. A line ends at LF,
 * CRLF or a lone CR (a CR at the end of a chunk waits to see whether an LF follows). A line is
 * decoded as UTF-8 only when it is whole, so a code point split across reads is never broken. Only
 * {@code event} and {@code data} are read. An event still incomplete when the stream ends is
 * dropped by never being finished.
 */
final class Sse {
  /** One dispatched event. */
  record Event(String name, String data) {}

  private final Consumer<Event> sink;
  private final ByteArrayOutputStream line = new ByteArrayOutputStream();
  private final StringBuilder data = new StringBuilder();
  private boolean hasData;
  private String name = "";
  private boolean afterCr;
  private boolean first = true;

  Sse(Consumer<Event> sink) {
    this.sink = sink;
  }

  /** Feeds bytes read from the stream. */
  void feed(byte[] bytes, int length) {
    for (int i = 0; i < length; i++) {
      byte b = bytes[i];
      if (afterCr) {
        afterCr = false;
        if (b == '\n') {
          continue;
        }
      }
      if (b == '\r') {
        afterCr = true;
        endLine();
      } else if (b == '\n') {
        endLine();
      } else {
        line.write(b);
      }
    }
  }

  private void endLine() {
    String text = Utf8.decode(line.toByteArray());
    line.reset();
    if (first) {
      first = false;
      if (text.startsWith("﻿")) {
        text = text.substring(1);
      }
    }
    if (text.isEmpty()) {
      dispatch();
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
      data.append(value).append('\n');
      hasData = true;
    }
  }

  private void dispatch() {
    if (hasData) {
      data.setLength(data.length() - 1);
      sink.accept(new Event(name.isEmpty() ? "message" : name, data.toString()));
    }
    data.setLength(0);
    hasData = false;
    name = "";
  }
}
