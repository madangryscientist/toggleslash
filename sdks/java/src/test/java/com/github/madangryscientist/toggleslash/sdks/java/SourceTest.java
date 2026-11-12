package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/**
 * A search of the SDK's own source for what the contract forbids: a regex engine, logging and
 * printing.
 */
final class SourceTest {
  private static final Path MAIN = Path.of("src/main/java");

  private static List<Path> sources() throws IOException {
    try (Stream<Path> walk = Files.walk(MAIN)) {
      return walk.filter(p -> p.toString().endsWith(".java")).toList();
    }
  }

  private static List<String> hits(Path root, List<String> needles) throws IOException {
    List<String> found = new ArrayList<>();
    try (Stream<Path> walk = Files.walk(root)) {
      for (Path path : walk.filter(p -> p.toString().endsWith(".java")).toList()) {
        String text = Files.readString(path);
        for (String needle : needles) {
          if (text.contains(needle)) {
            found.add(path + " uses " + needle);
          }
        }
      }
    }
    return found;
  }

  /**
   * The evaluator has its own {@code Pattern} class, a matcher without backtracking; the JDK's is
   * never used.
   */
  @Test
  void noRegexEngineAnywhereInTheSdk() throws IOException {
    assertTrue(sources().size() > 20);
    List<String> found =
        hits(
            MAIN,
            List.of(
                "import java.util.regex",
                "java.util.regex.",
                ".replaceAll(",
                ".replaceFirst(",
                "text.split(",
                ".split(\""));
    assertTrue(found.isEmpty(), found.toString());
  }

  @Test
  void noLoggingAndNoPrintingAnywhereInTheSdk() throws IOException {
    List<String> found =
        hits(
            MAIN,
            List.of(
                "java.util.logging",
                "System.Logger",
                "org.slf4j",
                "System.out",
                "System.err",
                "printStackTrace"));
    assertTrue(found.isEmpty(), found.toString());
  }

  @Test
  void theEvaluatorImportsNothingFromTheNetworkLayer() throws IOException {
    List<String> found =
        hits(
            MAIN.resolve("com/github/madangryscientist/toggleslash/sdks/java/evaluation"),
            List.of("java.net", "javax.net", "Client;", "Http;", "java.util.concurrent"));
    assertTrue(found.isEmpty(), found.toString());
  }
}
