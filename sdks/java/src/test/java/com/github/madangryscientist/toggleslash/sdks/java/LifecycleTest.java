package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.File;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

/** A client that is never closed does not keep the process from exiting. */
@Timeout(120)
final class LifecycleTest {
  @Test
  void theSdkThreadsAreDaemons() {
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          FakeServer.serving("\"v1\"", Support.ruleset(Support.boolFlag("checkout", true, 1)));
      Client client = Client.create(Support.options(server).build());
      assertTrue(client.ready());
      int found = 0;
      for (Thread thread : Thread.getAllStackTraces().keySet()) {
        if (thread.getName().startsWith("toggleslash-") && thread.isAlive()) {
          assertTrue(thread.isDaemon(), thread.getName() + " is a daemon");
          found++;
        }
      }
      assertTrue(found >= 2, "found " + found + " SDK threads");
      client.close();
    }
  }

  @Test
  void aProcessThatNeverClosesItsClientExits() throws Exception {
    try (FakeServer server = new FakeServer()) {
      server.ruleset =
          FakeServer.serving("\"v1\"", Support.ruleset(Support.boolFlag("checkout", true, 1)));
      server.stream = server.sse("event: connected\ndata: \"v1\"\n\n");
      String java =
          System.getProperty("java.home") + File.separator + "bin" + File.separator + "java";
      Process process =
          new ProcessBuilder(
                  java,
                  "-cp",
                  System.getProperty("java.class.path"),
                  NeverClosedMain.class.getName(),
                  server.url())
              .redirectErrorStream(true)
              .start();
      boolean exited = process.waitFor(30, TimeUnit.SECONDS);
      String output = new String(process.getInputStream().readAllBytes());
      if (!exited) {
        process.destroyForcibly();
      }
      assertTrue(exited, "the process exited; it printed: " + output);
      assertEquals(0, process.exitValue(), output);
      assertTrue(output.contains("main returns"), output);
      assertFalse(server.requests.isEmpty());
    }
  }
}
