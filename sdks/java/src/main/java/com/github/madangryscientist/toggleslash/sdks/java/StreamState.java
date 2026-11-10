package com.github.madangryscientist.toggleslash.sdks.java;

/** How the stream is doing, spelled as in the contract by {@link #value()}. */
public enum StreamState {
  /** {@code stream(false)}: polling only. */
  OFF("off"),
  /** Opening the stream, or the first load. */
  CONNECTING("connecting"),
  /** The server's {@code connected} event arrived. */
  CONNECTED("connected"),
  /** Down, waiting to retry (polling meanwhile). */
  DISCONNECTED("disconnected"),
  /** After {@code close()}. */
  CLOSED("closed");

  private final String value;

  StreamState(String value) {
    this.value = value;
  }

  /** The canonical spelling, as it appears in the contract and on the wire. */
  public String value() {
    return value;
  }

  @Override
  public String toString() {
    return value;
  }
}
