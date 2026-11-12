package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/** Runs every shared evaluation vector and every bucket pin from {@code fixtures/evaluation}. */
class VectorsTest {

  private static Path fixtures() {
    return Path.of(System.getProperty("toggleslash.fixtures", "../../fixtures"))
        .resolve("evaluation")
        .normalize();
  }

  private static Object read(Path file) throws IOException {
    return Json.parse(Files.readAllBytes(file));
  }

  /** The reason as the vectors spell it. */
  static Map<String, Object> reasonJson(Reason reason) {
    Map<String, Object> json = new LinkedHashMap<>();
    json.put("kind", reason.kind().name());
    switch (reason.kind()) {
      case RULE_MATCH -> json.put("ruleIndex", (double) reason.ruleIndex().getAsInt());
      case PREREQUISITE_FAILED -> json.put("flagKey", reason.flagKey());
      case ERROR -> json.put("code", reason.errorCode().name());
      default -> {}
    }
    return json;
  }

  /** The result as the vectors spell it. */
  static Map<String, Object> resultJson(EvaluationResult result) {
    Map<String, Object> json = new LinkedHashMap<>();
    json.put("value", result.value());
    json.put(
        "variationIndex",
        result.variationIndex().isPresent()
            ? (Object) (double) result.variationIndex().getAsInt()
            : null);
    json.put("reason", reasonJson(result.reason()));
    return json;
  }

  /** Deep equality by the spec: numbers by IEEE-754 (so -0 equals 0), objects by key set. */
  static boolean same(Object a, Object b) {
    if (a == null || b == null) {
      return a == b;
    }
    if (a instanceof Number x && b instanceof Number y) {
      return x.doubleValue() == y.doubleValue();
    }
    if (a instanceof Map<?, ?> x && b instanceof Map<?, ?> y) {
      if (!x.keySet().equals(y.keySet())) {
        return false;
      }
      for (Map.Entry<?, ?> entry : x.entrySet()) {
        if (!same(entry.getValue(), y.get(entry.getKey()))) {
          return false;
        }
      }
      return true;
    }
    if (a instanceof List<?> x && b instanceof List<?> y) {
      if (x.size() != y.size()) {
        return false;
      }
      for (int i = 0; i < x.size(); i++) {
        if (!same(x.get(i), y.get(i))) {
          return false;
        }
      }
      return true;
    }
    return a.equals(b);
  }

  @Test
  void everyVectorAndPin() throws IOException {
    Path dir = fixtures();
    List<Path> files;
    try (Stream<Path> listing = Files.list(dir)) {
      files = listing.filter(p -> p.toString().endsWith(".json")).sorted().toList();
    }
    assertTrue(!files.isEmpty(), "no fixture files in " + dir);
    int vectors = 0;
    int pins = 0;
    List<String> failures = new ArrayList<>();
    for (Path file : files) {
      String name = file.getFileName().toString();
      Map<?, ?> document = (Map<?, ?>) read(file);
      if (name.equals("buckets.json")) {
        for (Object item : (List<?>) document.get("buckets")) {
          Map<?, ?> pin = (Map<?, ?>) item;
          String key = (String) pin.get("flagKey");
          String salt = (String) pin.get("salt");
          String contextKey = (String) pin.get("contextKey");
          pins++;
          long uint32 = Bucket.uint32(key, salt, contextKey);
          int bucket = Bucket.of(key, salt, contextKey);
          if (uint32 != ((Number) pin.get("uint32")).longValue()
              || bucket != ((Number) pin.get("bucket")).intValue()) {
            failures.add(
                "%s pin %s/%s/%s: got uint32 %d bucket %d"
                    .formatted(name, key, salt, contextKey, uint32, bucket));
          }
        }
        continue;
      }
      for (Object item : (List<?>) document.get("vectors")) {
        Map<?, ?> vector = (Map<?, ?>) item;
        vectors++;
        Map<String, Object> input = new LinkedHashMap<>();
        input.put("flags", vector.get("flags"));
        input.put("segments", vector.get("segments"));
        EvaluationResult result =
            Evaluator.evaluate(
                Ruleset.load(input), (String) vector.get("flagKey"), vector.get("context"));
        Map<String, Object> actual = resultJson(result);
        if (!same(actual, vector.get("expected"))) {
          failures.add(
              "%s vector '%s': expected %s, got %s"
                  .formatted(name, vector.get("name"), vector.get("expected"), actual));
        }
      }
    }
    String summary = "vectors " + vectors + " pins " + pins;
    System.out.println("evaluation: " + summary);
    String counts = System.getProperty("toggleslash.counts");
    if (counts != null) {
      Files.writeString(Path.of(counts), "native: " + summary + "\n", StandardCharsets.UTF_8);
    }
    assertTrue(vectors > 0 && pins > 0, "read no vectors or no pins");
    assertEquals(List.of(), failures);
  }
}
