package com.github.madangryscientist.toggleslash.sdks.java;

/** Run in a child JVM by {@link ClientTest}: makes a client, never closes it, and returns. */
public final class UnclosedMain {
  private UnclosedMain() {}

  /** The one argument is the server's URL. */
  public static void main(String[] args) {
    Client client = Client.create(Options.builder(args[0], "fs_server_x").build());
    client.ready();
    System.out.println("returned");
  }
}
