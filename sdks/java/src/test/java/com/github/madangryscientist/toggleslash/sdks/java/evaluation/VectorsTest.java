package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/**
 * Runs every vector in {@code fixtures/evaluation/} (read from the repository, never a copy) and
 * every pin in {@code buckets.json} through the evaluator.
 */
class VectorsTest {
  private static final Path DIR = Path.of("../../fixtures/evaluation");

  private static Object read(Path file) throws IOException {
    return Json.parse(Files.readAllBytes(file));
  }

  private static List<Path> files() throws IOException {
    try (Stream<Path> stream = Files.list(DIR)) {
      return stream.filter(p -> p.toString().endsWith(".json")).sorted().toList();
    }
  }

  /** The result as the JSON the vectors spell it. */
  private static Map<String, Object> asJson(Result result) {
    Map<String, Object> reason = new LinkedHashMap<>();
    Reason r = result.reason();
    reason.put("kind", r.kind().value());
    r.ruleIndex().ifPresent(i -> reason.put("ruleIndex", (double) i));
    r.flagKey().ifPresent(k -> reason.put("flagKey", k));
    r.code().ifPresent(c -> reason.put("code", c.value()));
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("value", result.value());
    out.put(
        "variationIndex",
        result.variationIndex().isPresent()
            ? (Object) (double) result.variationIndex().getAsInt()
            : null);
    out.put("reason", reason);
    return out;
  }

  /** Deep equality by the specification's rules: numbers by IEEE-754, so -0 equals 0. */
  static boolean same(Object a, Object b) {
    if (a instanceof Double x && b instanceof Double y) {
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
    return a == null ? b == null : a.equals(b);
  }

  @Test
  void everyVectorAndEveryPin() throws IOException {
    int vectors = 0;
    int pins = 0;
    List<String> failures = new ArrayList<>();
    for (Path file : files()) {
      String name = file.getFileName().toString();
      Map<?, ?> doc = (Map<?, ?>) read(file);
      if (name.equals("buckets.json")) {
        for (Object item : (List<?>) doc.get("buckets")) {
          Map<?, ?> pin = (Map<?, ?>) item;
          String flagKey = (String) pin.get("flagKey");
          String salt = (String) pin.get("salt");
          String contextKey = (String) pin.get("contextKey");
          long uint32 = Contexts.uint32(flagKey, salt, contextKey);
          int bucket = Contexts.bucket(flagKey, salt, contextKey);
          if (uint32 != ((Double) pin.get("uint32")).longValue()
              || bucket != ((Double) pin.get("bucket")).intValue()) {
            failures.add(name + ": " + flagKey + "/" + salt + "/" + contextKey);
          }
          pins++;
        }
        continue;
      }
      for (Object item : (List<?>) doc.get("vectors")) {
        Map<?, ?> vector = (Map<?, ?>) item;
        Map<String, Object> input = new LinkedHashMap<>();
        input.put("flags", vector.get("flags"));
        input.put("segments", vector.get("segments"));
        Ruleset ruleset = Ruleset.of(input);
        Result result = ruleset.evaluate((String) vector.get("flagKey"), vector.get("context"));
        Map<String, Object> actual = asJson(result);
        Object expected = vector.get("expected");
        if (!same(actual, expected)) {
          failures.add(
              name
                  + ": "
                  + vector.get("name")
                  + "\n  expected "
                  + Json.write(expected)
                  + "\n  actual   "
                  + Json.write(actual));
        }
        vectors++;
      }
    }
    System.out.println("vectors: " + vectors + " pins: " + pins);
    assertTrue(
        failures.isEmpty(), () -> failures.size() + " failed:\n" + String.join("\n", failures));
    assertTrue(vectors > 0, "read no vectors");
    assertTrue(pins > 0, "read no pins");
  }

  @Test
  void aLoneSurrogateIsHashedAsReplacement() {
    assertEquals(Contexts.uint32("f", "s", "a�b"), Contexts.uint32("f", "s", "a\uD800b"));
    assertFalse(Contexts.uint32("f", "s", "a?b") == Contexts.uint32("f", "s", "a\uD800b"));
  }
}
