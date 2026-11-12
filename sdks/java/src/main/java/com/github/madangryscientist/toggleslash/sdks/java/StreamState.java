package com.github.madangryscientist.toggleslash.sdks.java;

/** How the stream is doing, to section 8 of the contract. */
public enum StreamState {
  /** {@code stream} is off; it stays off until {@code close}. */
  OFF("off"),
  /** The first load, and each stream attempt while it is opening. */
  CONNECTING("connecting"),
  /** The stream's {@code connected} event arrived and the stream is still open. */
  CONNECTED("connected"),
  /** The last stream attempt ended and the SDK is waiting to retry, polling meanwhile. */
  DISCONNECTED("disconnected"),
  /** After {@code close} was called, whatever it was before. */
  CLOSED("closed");

  private final String wire;

  StreamState(String wire) {
    this.wire = wire;
  }

  /** The lowercase spelling of the contract: {@code "off"}, {@code "connecting"} and so on. */
  public String wire() {
    return wire;
  }
}
