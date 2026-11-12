package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.concurrent.TimeoutException;
import java.util.function.BooleanSupplier;

/** Small helpers shared by the native tests. */
final class Support {
  private Support() {}

  /** One boolean flag that serves its fallthrough variation. */
  static String boolFlag(String key, boolean serves, long version) {
    return flag(key, "[false,true]", serves ? 1 : 0, "[]", version);
  }

  /** A flag with the given variations JSON, a fallthrough index and rules JSON. */
  static String flag(String key, String variations, int fallthrough, String rules, long version) {
    return "{\"key\":\""
        + key
        + "\",\"variations\":"
        + variations
        + ",\"on\":true,\"offVariation\":0,\"targets\":[],\"prerequisites\":[],\"rules\":"
        + rules
        + ",\"fallthrough\":{\"variation\":"
        + fallthrough
        + "},\"salt\":\"s\",\"version\":"
        + version
        + "}";
  }

  /** A ruleset of the flags. */
  static String ruleset(String... flags) {
    return "{\"flags\":[" + String.join(",", flags) + "],\"segments\":[]}";
  }

  /** Waits until the condition holds. */
  static void until(String what, long millis, BooleanSupplier condition) {
    long end = System.nanoTime() + millis * 1_000_000L;
    while (!condition.getAsBoolean()) {
      if (System.nanoTime() > end) {
        throw new AssertionError(
            new TimeoutException("not within " + millis + " ms: " + what).getMessage());
      }
      try {
        Thread.sleep(5);
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
        throw new AssertionError(e);
      }
    }
  }

  static Options.Builder options(FakeServer server) {
    return Options.builder(server.url(), FakeServer.KEY);
  }
}
