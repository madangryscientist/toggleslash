package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.Locale;

/** How the stream is doing (contract section 8). */
public enum StreamState {
  /** The stream option is off. It stays off until close. */
  OFF,
  /** The first load, or a stream attempt that is opening. */
  CONNECTING,
  /** The stream's {@code connected} event arrived and the stream is still open. */
  CONNECTED,
  /** The last stream attempt ended and the SDK is waiting to retry. */
  DISCONNECTED,
  /** {@link ToggleSlashClient#close()} has been called. */
  CLOSED;

  /**
   * The canonical lower-case spelling.
   *
   * @return {@code off}, {@code connecting}, {@code connected}, {@code disconnected} or {@code
   *     closed}
   */
  public String value() {
    return name().toLowerCase(Locale.ROOT);
  }

  @Override
  public String toString() {
    return value();
  }
}
