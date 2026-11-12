package com.github.madangryscientist.toggleslash.sdks.java.internal;

import java.util.List;

/**
 * A Server-Sent Events parser by the WHATWG rules: LF, CRLF and a lone CR end a line, a line that
 * starts with a colon is a comment, and only the {@code event} and {@code data} fields are read.
 * Text may arrive in any split; an event still incomplete at the end is never dispatched.
 */
public final class SseParser {
  /**
   * One dispatched event.
   *
   * @param name the event name, {@code message} when none was given
   * @param data the data lines joined with LF
   */
  public record Event(String name, String data) {}

  private final StringBuilder line = new StringBuilder();
  private final StringBuilder data = new StringBuilder();
  private boolean hasData;
  private String eventName = "";
  private boolean afterCr;
  private boolean atStart = true;

  /**
   * Feeds decoded text and collects the events it completes.
   *
   * @param text the next piece of the stream
   * @param out receives each completed event, in order
   */
  public void feed(CharSequence text, List<Event> out) {
    for (int i = 0; i < text.length(); i++) {
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
        afterCr = true;
        endLine(out);
      } else if (c == '\n') {
        endLine(out);
      } else {
        line.append(c);
      }
    }
  }

  private void endLine(List<Event> out) {
    String text = line.toString();
    line.setLength(0);
    if (text.isEmpty()) {
      dispatch(out);
      return;
    }
    if (text.charAt(0) == ':') {
      return;
    }
    int colon = text.indexOf(':');
    String field;
    String value;
    if (colon < 0) {
      field = text;
      value = "";
    } else {
      field = text.substring(0, colon);
      value = text.substring(colon + 1);
      if (value.startsWith(" ")) {
        value = value.substring(1);
      }
    }
    if (field.equals("event")) {
      eventName = value;
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
      out.add(new Event(eventName.isEmpty() ? "message" : eventName, data.toString()));
    }
    data.setLength(0);
    hasData = false;
    eventName = "";
  }
}
