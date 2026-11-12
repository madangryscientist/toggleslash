package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/** Searches over the SDK's own source for things it must not do. */
class SourceSearchTest {
  private static final Path MAIN = Path.of("src/main/java");

  private static List<String> lines(Path root, String contains) throws IOException {
    List<String> hits = new ArrayList<>();
    try (Stream<Path> files = Files.walk(root)) {
      for (Path file : files.filter(p -> p.toString().endsWith(".java")).toList()) {
        List<String> all = Files.readAllLines(file);
        for (int i = 0; i < all.size(); i++) {
          if (all.get(i).contains(contains)) {
            hits.add(file + ":" + (i + 1) + ": " + all.get(i).trim());
          }
        }
      }
    }
    return hits;
  }

  @Test
  void theEvaluatorUsesNoPlatformRegex() throws IOException {
    Path evaluation = MAIN.resolve("com/github/madangryscientist/toggleslash/sdks/java/evaluation");
    for (String needle :
        new String[] {
          "java.util.regex", "Pattern.", ".matches(\"", ".replaceAll(", ".replaceFirst("
        }) {
      assertEquals(List.of(), lines(evaluation, needle), needle);
    }
    // String.split takes a regular expression, so the evaluator splits by hand (Text.split).
    for (String hit : lines(evaluation, ".split(")) {
      assertTrue(hit.contains("Text.split("), hit);
    }
    assertEquals(List.of(), lines(evaluation, "String.split"));
  }

  @Test
  void nothingIsLoggedOrPrinted() throws IOException {
    for (String needle :
        new String[] {
          "System.out", "System.err", "java.util.logging", "printStackTrace", "Logger"
        }) {
      assertEquals(List.of(), lines(MAIN, needle), needle);
    }
  }

  @Test
  void noErrorTheSdkBuildsNamesAContextOrAKey() throws IOException {
    List<String> builders = new ArrayList<>(lines(MAIN, "new ToggleSlashException("));
    builders.addAll(lines(MAIN, "report("));
    assertTrue(builders.size() > 5);
    for (String hit : builders) {
      String lower = hit.toLowerCase(java.util.Locale.ROOT);
      assertTrue(
          !lower.contains("context") && !lower.contains("sdkkey") && !lower.contains("flagkey"),
          hit);
    }
  }
}
