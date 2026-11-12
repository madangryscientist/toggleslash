package com.github.madangryscientist.toggleslash.sdks.java;

/** How the stream is doing, to section 8 of the SDK contract. */
public enum StreamState {
  /** The {@code stream} option is off. It stays off until {@code close}. */
  OFF("off"),
  /** The first load, and each stream attempt while it is opening. */
  CONNECTING("connecting"),
  /** The stream's {@code connected} event arrived and the stream is still open. */
  CONNECTED("connected"),
  /** The last stream attempt ended and the SDK is waiting to retry. */
  DISCONNECTED("disconnected"),
  /** {@code close} has been called. */
  CLOSED("closed");

  private final String value;

  StreamState(String value) {
    this.value = value;
  }

  /**
   * The canonical lower-case spelling.
   *
   * @return the spelling the contract uses
   */
  public String value() {
    return value;
  }
}
