package com.github.madangryscientist.toggleslash.sdks.java.internal;

import java.net.http.HttpClient;
import java.time.Duration;

/** Knobs only a test turns: the watchdog length, the clock and the HTTP client. */
public final class Tuning {
  /** The stream watchdog; the contract fixes it at 60 seconds. */
  public Duration watchdog = Duration.ofSeconds(60);

  /** The clock. */
  public Clock clock = Clock.SYSTEM;

  /** An HTTP client to use instead of the SDK's own, or null. */
  public HttpClient http;
}
