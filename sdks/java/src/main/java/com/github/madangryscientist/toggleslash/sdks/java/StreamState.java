package com.github.madangryscientist.toggleslash.sdks.java;

/** The state of the stream, spelled as the contract spells it. */
public enum StreamState {
  /** The stream is not in use. */
  off,
  /** An attempt is opening. */
  connecting,
  /** The stream is open. */
  connected,
  /** The last attempt ended. */
  disconnected,
  /** The client is closed. */
  closed
}
