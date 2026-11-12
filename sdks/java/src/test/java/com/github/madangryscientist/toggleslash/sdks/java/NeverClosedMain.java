package com.github.madangryscientist.toggleslash.sdks.java;

import java.time.Duration;

/** Creates a client, never closes it and returns: the process must still exit. */
public final class NeverClosedMain {
  private NeverClosedMain() {}

  /**
   * Runs.
   *
   * @param args the server URL
   */
  public static void main(String[] args) {
    Client client =
        Client.create(
            Options.builder(args[0], "key")
                .pollInterval(Duration.ofMillis(50))
                .flushInterval(Duration.ofMillis(50))
                .build());
    client.ready();
    System.out.println("main returns");
  }
}
