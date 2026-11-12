package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/** Searches the source for things the spec forbids. */
class SourceAuditTest {
  private static final Path MAIN = Path.of("src", "main", "java");

  private static List<Path> sources(Path root) throws IOException {
    try (Stream<Path> walk = Files.walk(root)) {
      return walk.filter(p -> p.toString().endsWith(".java")).toList();
    }
  }

  @Test
  void theEvaluatorUsesNoPlatformRegex() throws IOException {
    List<String> hits = new ArrayList<>();
    String[] banned = {
      "java.util.regex", "Pattern.", ".matches(", ".split(", ".replaceAll(", ".replaceFirst("
    };
    for (Path file :
        sources(MAIN.resolve("com/github/madangryscientist/toggleslash/sdks/java/evaluation"))) {
      List<String> lines = Files.readAllLines(file);
      for (int i = 0; i < lines.size(); i++) {
        String line = lines.get(i).trim();
        if (line.startsWith("*") || line.startsWith("//") || line.startsWith("/*")) {
          continue;
        }
        for (String word : banned) {
          if (line.contains(word)) {
            hits.add(file + ":" + (i + 1) + " " + word);
          }
        }
      }
    }
    assertTrue(hits.isEmpty(), hits.toString());
  }

  @Test
  void nothingIsPrintedOrLoggedByTheLibrary() throws IOException {
    List<String> hits = new ArrayList<>();
    for (Path file : sources(MAIN)) {
      List<String> lines = Files.readAllLines(file);
      for (int i = 0; i < lines.size(); i++) {
        String line = lines.get(i);
        if (line.contains("System.out")
            || line.contains("System.err")
            || line.contains("printStackTrace")
            || line.contains("java.util.logging")) {
          hits.add(file + ":" + (i + 1));
        }
      }
    }
    assertTrue(hits.isEmpty(), hits.toString());
  }

  @Test
  void noErrorValueIsBuiltFromContextData() throws IOException {
    List<String> hits = new ArrayList<>();
    for (Path file : sources(MAIN)) {
      List<String> lines = Files.readAllLines(file);
      for (int i = 0; i < lines.size(); i++) {
        String line = lines.get(i);
        boolean error =
            line.contains("new ToggleSlashException(")
                || line.contains("report(")
                || line.contains("fail(");
        if (error && (line.toLowerCase().contains("context") || line.contains("attributes"))) {
          hits.add(file + ":" + (i + 1) + " " + line.trim());
        }
      }
    }
    assertTrue(hits.isEmpty(), hits.toString());
  }

  @Test
  void seamsAreNeverGlobalsOrEnvironmentVariables() throws IOException {
    List<String> hits = new ArrayList<>();
    for (Path file : sources(MAIN)) {
      List<String> lines = Files.readAllLines(file);
      for (int i = 0; i < lines.size(); i++) {
        String line = lines.get(i);
        if (line.contains("System.getenv") || line.contains("System.getProperty")) {
          hits.add(file + ":" + (i + 1));
        }
      }
    }
    assertTrue(hits.isEmpty(), hits.toString());
  }
}
