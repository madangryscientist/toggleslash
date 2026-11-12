package com.github.madangryscientist.toggleslash.sdks.java;

/** Where the stream to the server stands (contract section 8). */
public enum StreamState {
  /** {@code stream} is off. It stays off until {@code close}. */
  OFF,
  /** The first load, and each stream attempt while it is opening. */
  CONNECTING,
  /** The stream's {@code connected} event arrived and the stream is still open. */
  CONNECTED,
  /** The last stream attempt ended and the SDK is waiting to retry. */
  DISCONNECTED,
  /** {@code close} has been called. */
  CLOSED;

  /** The canonical lower case spelling of the contract ({@code "off"}, {@code "connected"}). */
  public String wire() {
    return name().toLowerCase(java.util.Locale.ROOT);
  }
}
