package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.junit.jupiter.api.Assertions.fail;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/** Runs every evaluation vector and every bucket pin from {@code fixtures/evaluation}. */
class VectorsTest {
  private static final Path FIXTURES = Path.of("..", "..", "fixtures", "evaluation");

  private static Object read(Path file) throws IOException {
    return Json.parse(Utf8.decode(Files.readAllBytes(file)));
  }

  private static List<Path> files() throws IOException {
    try (Stream<Path> stream = Files.list(FIXTURES)) {
      return stream.filter(p -> p.toString().endsWith(".json")).sorted().toList();
    }
  }

  /** Deep equality by the spec: numbers by IEEE-754, so -0 equals 0. */
  static boolean same(Object a, Object b) {
    if (a == null || b == null) {
      return a == b;
    }
    if (a instanceof Double && b instanceof Double) {
      return ((Double) a).doubleValue() == ((Double) b).doubleValue();
    }
    if (a instanceof List<?> && b instanceof List<?>) {
      List<?> x = (List<?>) a;
      List<?> y = (List<?>) b;
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
    if (a instanceof Map<?, ?> && b instanceof Map<?, ?>) {
      Map<?, ?> x = (Map<?, ?>) a;
      Map<?, ?> y = (Map<?, ?>) b;
      if (!x.keySet().equals(y.keySet())) {
        return false;
      }
      for (Object key : x.keySet()) {
        if (!same(x.get(key), y.get(key))) {
          return false;
        }
      }
      return true;
    }
    return a.equals(b);
  }

  private static String describe(Result result) {
    Reason r = result.reason();
    return "{value="
        + Json.write(result.value())
        + ", variationIndex="
        + result.variationIndex()
        + ", reason="
        + r
        + "}";
  }

  private static String check(Map<?, ?> vector, Result result) {
    Map<?, ?> expected = (Map<?, ?>) vector.get("expected");
    Map<?, ?> reason = (Map<?, ?>) expected.get("reason");
    Object index = expected.get("variationIndex");
    boolean indexOk =
        index == null
            ? result.variationIndex() == null
            : result.variationIndex() != null
                && result.variationIndex().doubleValue() == (Double) index;
    Reason actual = result.reason();
    boolean reasonOk =
        actual.kind().name().equals(reason.get("kind"))
            && same(
                actual.errorCode() == null ? null : actual.errorCode().name(), reason.get("code"))
            && same(
                actual.ruleIndex() == null ? null : Double.valueOf(actual.ruleIndex()),
                reason.get("ruleIndex"))
            && same(actual.flagKey(), reason.get("flagKey"));
    if (indexOk && reasonOk && same(result.value(), expected.get("value"))) {
      return null;
    }
    return "expected " + Json.write(expected) + " but got " + describe(result);
  }

  @Test
  void everyVectorAndEveryPin() throws IOException {
    int vectors = 0;
    int pins = 0;
    int fileCount = 0;
    List<String> failures = new ArrayList<>();
    for (Path file : files()) {
      fileCount++;
      Map<?, ?> content = (Map<?, ?>) read(file);
      String name = file.getFileName().toString();
      if (name.equals("buckets.json")) {
        for (Object item : (List<?>) content.get("buckets")) {
          Map<?, ?> pin = (Map<?, ?>) item;
          long expectedUint = (long) ((Double) pin.get("uint32")).doubleValue();
          int bucket =
              Bucket.of(
                  (String) pin.get("flagKey"),
                  (String) pin.get("salt"),
                  (String) pin.get("contextKey"));
          long uint =
              Bucket.uint32(
                  (String) pin.get("flagKey"),
                  (String) pin.get("salt"),
                  (String) pin.get("contextKey"));
          pins++;
          if (uint != expectedUint || bucket != (int) ((Double) pin.get("bucket")).doubleValue()) {
            failures.add(name + " pin " + pin + ": got uint32 " + uint + " bucket " + bucket);
          }
        }
        continue;
      }
      for (Object item : (List<?>) content.get("vectors")) {
        Map<?, ?> vector = (Map<?, ?>) item;
        vectors++;
        Map<String, Object> input = new java.util.LinkedHashMap<>();
        input.put("flags", vector.get("flags"));
        input.put("segments", vector.get("segments"));
        Result result =
            Evaluator.evaluate(
                Ruleset.parse(input), (String) vector.get("flagKey"), vector.get("context"));
        String problem = check(vector, result);
        if (problem != null) {
          failures.add(name + " / " + vector.get("name") + ": " + problem);
        }
      }
    }
    assertTrue(fileCount > 0, "no fixture files were read");
    assertTrue(vectors > 0, "no vectors were read");
    assertTrue(pins > 0, "no pins were read");
    System.out.println("vectors: " + vectors + " pins: " + pins);
    if (!failures.isEmpty()) {
      fail(failures.size() + " failed:\n" + String.join("\n", failures));
    }
    assertEquals(0, failures.size());
  }
}
