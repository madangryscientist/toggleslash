package com.github.madangryscientist.toggleslash.sdks.java;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Utf8Decoder;
import java.util.function.BiConsumer;

/**
 * A Server-Sent Events parser as the WHATWG standard reads one: LF, CRLF and a lone CR end a line,
 * a line starting with {@code :} is a comment, a leading byte order mark is skipped, and UTF-8 and
 * line endings may be split across reads. Only {@code event} and {@code data} are read. An event
 * with no {@code data} line is dropped, and one still open at the end of the stream is discarded.
 * Not thread safe: one stream's reader owns it.
 */
final class SseParser {

  private final Utf8Decoder decoder = new Utf8Decoder();
  private final StringBuilder chars = new StringBuilder();
  private final StringBuilder line = new StringBuilder();
  private final StringBuilder data = new StringBuilder();
  private final BiConsumer<String, String> sink;
  private String name = "";
  private boolean hasData;
  private boolean lastWasCr;
  private boolean first = true;

  /** A parser that hands each event to {@code sink} as (name, data). */
  SseParser(BiConsumer<String, String> sink) {
    this.sink = sink;
  }

  /** Feeds bytes read from the stream. */
  void feed(byte[] bytes, int offset, int length) {
    chars.setLength(0);
    decoder.feed(bytes, offset, length, chars);
    consume();
  }

  private void consume() {
    int from = 0;
    if (first && chars.length() > 0) {
      first = false;
      if (chars.charAt(0) == '﻿') {
        from = 1;
      }
    }
    for (int i = from; i < chars.length(); i++) {
      char c = chars.charAt(i);
      if (lastWasCr) {
        lastWasCr = false;
        if (c == '\n') {
          continue;
        }
      }
      if (c == '\r') {
        lastWasCr = true;
        endOfLine();
      } else if (c == '\n') {
        endOfLine();
      } else {
        line.append(c);
      }
    }
  }

  private void endOfLine() {
    String text = line.toString();
    line.setLength(0);
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
      if (hasData) {
        data.append('\n');
      }
      data.append(value);
      hasData = true;
    }
  }

  private void dispatch() {
    if (hasData) {
      String eventName = name.isEmpty() ? "message" : name;
      String eventData = data.toString();
      name = "";
      data.setLength(0);
      hasData = false;
      sink.accept(eventName, eventData);
    } else {
      name = "";
    }
  }
}
