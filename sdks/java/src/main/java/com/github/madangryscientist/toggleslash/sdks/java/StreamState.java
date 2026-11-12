package com.github.madangryscientist.toggleslash.sdks.java;

/** The state of the stream, as {@code status} reports it. */
public enum StreamState {
  /** The stream option is off. */
  OFF("off"),
  /** The first load, or a stream attempt that is opening. */
  CONNECTING("connecting"),
  /** The stream's connected event arrived and the stream is open. */
  CONNECTED("connected"),
  /** The last stream attempt ended and the SDK is waiting to retry. */
  DISCONNECTED("disconnected"),
  /** Close has been called. */
  CLOSED("closed");

  private final String wire;

  StreamState(String wire) {
    this.wire = wire;
  }

  /**
   * The contract's spelling: lower case.
   *
   * @return the spelling
   */
  public String wire() {
    return wire;
  }

  @Override
  public String toString() {
    return wire;
  }
}
