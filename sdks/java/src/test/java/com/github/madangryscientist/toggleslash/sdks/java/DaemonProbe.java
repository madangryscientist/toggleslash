package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Duration;

/**
 * Run in a child JVM by {@link ClientTest}: makes a client, never closes it and returns from {@code
 * main}. The JVM must then exit by itself, because the SDK's threads are daemon threads.
 */
public final class DaemonProbe {
  private DaemonProbe() {}

  /**
   * Starts a client against a port nothing listens on and returns.
   *
   * @param args ignored
   */
  public static void main(String[] args) {
    Client client =
        Client.create(
            Options.builder("http://127.0.0.1:9", "key").stream(false)
                .pollInterval(Duration.ofMillis(50))
                .build());
    client.ready();
    System.out.println("probe done");
  }
}
