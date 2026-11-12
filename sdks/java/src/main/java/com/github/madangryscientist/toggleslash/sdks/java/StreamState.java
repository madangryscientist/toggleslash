package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * Where the stream is. The constants are the contract's states in upper case; {@link #value()} is
 * the canonical lower-case spelling.
 */
public enum StreamState {
  /** The {@code stream} option is off. */
  OFF("off"),
  /** The first load, or a stream attempt that is opening. */
  CONNECTING("connecting"),
  /** The server's {@code connected} event arrived and the stream is still open. */
  CONNECTED("connected"),
  /** The last attempt ended and the client is waiting to retry. */
  DISCONNECTED("disconnected"),
  /** {@code close} has begun. */
  CLOSED("closed");

  private final String value;

  StreamState(String value) {
    this.value = value;
  }

  /**
   * The canonical spelling.
   *
   * @return {@code off}, {@code connecting}, {@code connected}, {@code disconnected} or {@code
   *     closed}
   */
  public String value() {
    return value;
  }
}
