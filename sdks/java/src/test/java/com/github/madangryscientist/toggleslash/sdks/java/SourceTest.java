package com.github.madangryscientist.toggleslash.sdks.java;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.regex.Pattern;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/** Source searches for what the harness cannot see: logs, error values and the platform's regex. */
class SourceTest {
  private static final Path MAIN =
      Path.of("src/main/java/com/github/madangryscientist/toggleslash/sdks/java");

  private static List<Path> sources(Path root) throws IOException {
    try (Stream<Path> walk = Files.walk(root)) {
      return walk.filter(p -> p.toString().endsWith(".java")).sorted().toList();
    }
  }

  private static List<String> find(Path root, Pattern pattern) throws IOException {
    List<String> hits = new ArrayList<>();
    for (Path file : sources(root)) {
      List<String> lines = Files.readAllLines(file);
      for (int i = 0; i < lines.size(); i++) {
        String line = lines.get(i).strip();
        boolean comment = line.startsWith("*") || line.startsWith("//") || line.startsWith("/*");
        if (!comment && pattern.matcher(line).find()) {
          hits.add(file + ":" + (i + 1) + ": " + line);
        }
      }
    }
    return hits;
  }

  @Test
  void theSdkWritesNoLogLine() throws IOException {
    Pattern logging =
        Pattern.compile(
            "System\\.(out|err|getLogger)|java\\.util\\.logging|org\\.slf4j|printStackTrace|\\bLogger\\b");
    assertEquals(List.of(), find(MAIN, logging));
  }

  @Test
  void noErrorValueIsBuiltFromAContext() throws IOException {
    // Every error value is built in Core.fail, Usage and Client from a fixed text and a code. A
    // line
    // that builds an exception or an onError message from a context, a key or an attribute would
    // name one of them here.
    Pattern built =
        Pattern.compile(
            "new (ToggleSlashException|IllegalStateException|RuntimeException)\\(.*(context|attribute|\\bkey\\b)");
    assertEquals(List.of(), find(MAIN, built));
    Pattern failing = Pattern.compile("(fail|report)\\(.*(context|attribute)");
    assertEquals(List.of(), find(MAIN, failing));
  }

  @Test
  void theEvaluatorUsesNoPlatformRegex() throws IOException {
    Path evaluation = MAIN.resolve("evaluation");
    Pattern regex =
        Pattern.compile(
            "java\\.util\\.regex|\\bPattern\\.(compile|matches)|\\bMatcher\\b|(?<!Patterns)\\.matches\\(|\\.replaceAll\\(|\\.replaceFirst\\(|\\.split\\(|\\.splitAsStream\\(");
    assertEquals(List.of(), find(evaluation, regex));
    assertTrue(Files.exists(evaluation.resolve("Patterns.java")));
  }

  @Test
  void theSdkSendsOnlyTheProtocolsHeaders() throws IOException {
    // A request header is set with a name and a value; a call with a name alone reads a response.
    Pattern name = Pattern.compile("\\.header\\(\"([^\"]+)\",");
    List<String> hits = find(MAIN, name);
    List<String> names = new ArrayList<>();
    for (String hit : hits) {
      var matcher = name.matcher(hit);
      while (matcher.find()) {
        names.add(matcher.group(1));
      }
    }
    for (String header : names) {
      assertTrue(
          List.of("Authorization", "Accept", "If-None-Match", "Content-Type").contains(header),
          "an unexpected request header: " + header);
    }
    assertTrue(names.size() >= 4);
  }
}
