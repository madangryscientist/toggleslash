package com.github.madangryscientist.toggleslash.sdks.java;

/** Starts a client against a server, uses it and returns without closing it. */
public final class UnclosedClientMain {
  private UnclosedClientMain() {}

  /**
   * Runs the program.
   *
   * @param args the server URL
   */
  public static void main(String[] args) {
    Client client = Client.create(new Options().url(args[0]).sdkKey("k").stream(true));
    boolean ready = client.ready();
    boolean value = client.boolVariation("f", Context.user("u"), false);
    System.out.println("served " + (ready && value));
  }
}
