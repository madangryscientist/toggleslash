package com.github.madangryscientist.toggleslash.sdks.java;

/** How the live update stream is doing. */
public enum StreamState {
  /** The stream is switched off: the SDK polls. */
  OFF("off"),
  /** The first load, or an open in progress. */
  CONNECTING("connecting"),
  /** The server's {@code connected} event arrived. */
  CONNECTED("connected"),
  /** The stream is down and the SDK waits to retry, polling meanwhile. */
  DISCONNECTED("disconnected"),
  /** The client was closed. */
  CLOSED("closed");

  private final String value;

  StreamState(String value) {
    this.value = value;
  }

  /**
   * The canonical lower case word.
   *
   * @return {@code off}, {@code connecting}, {@code connected}, {@code disconnected} or {@code
   *     closed}
   */
  public String value() {
    return value;
  }

  @Override
  public String toString() {
    return value;
  }
}
