package com.github.madangryscientist.toggleslash.sdks.java;

/** How the live update stream is doing; the contract's {@code stream} status values. */
public enum StreamState {
  /** {@code stream} is off; it stays off until {@code close}. */
  OFF("off"),
  /** The first load, or a stream attempt that is still opening. */
  CONNECTING("connecting"),
  /** The stream's {@code connected} event arrived and the stream is still open. */
  CONNECTED("connected"),
  /** The last stream attempt ended and the SDK is waiting to retry, polling meanwhile. */
  DISCONNECTED("disconnected"),
  /** After {@code close} has been called, whatever it was before. */
  CLOSED("closed");

  private final String value;

  StreamState(String value) {
    this.value = value;
  }

  /**
   * The contract's spelling of this state, in lower case.
   *
   * @return the spelling, such as {@code "connected"}
   */
  public String value() {
    return value;
  }

  @Override
  public String toString() {
    return value;
  }
}
