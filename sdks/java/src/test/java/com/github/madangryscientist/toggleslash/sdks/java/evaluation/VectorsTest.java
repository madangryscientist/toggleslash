package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.junit.jupiter.api.Assertions.fail;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;

/**
 * Runs every shared evaluation vector and every bucket pin from {@code fixtures/evaluation}, read
 * in place (never copied), through the SDK's own JSON reader and evaluator.
 */
class VectorsTest {
  private static final Path FIXTURES = Path.of("../../fixtures/evaluation");

  private static int vectors;
  private static int pins;

  private static List<Path> files() throws IOException {
    try (Stream<Path> stream = Files.list(FIXTURES)) {
      return stream.filter(p -> p.toString().endsWith(".json")).sorted().toList();
    }
  }

  private static Object read(Path file) throws IOException {
    return Json.parse(Utf8.decode(Files.readAllBytes(file)));
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> object(Object value) {
    return (Map<String, Object>) value;
  }

  @SuppressWarnings("unchecked")
  private static List<Object> array(Object value) {
    return (List<Object>) value;
  }

  private static boolean same(Object expected, Object actual) {
    if (expected instanceof Double e && actual instanceof Double a) {
      return e.doubleValue() == a.doubleValue();
    }
    if (expected instanceof List<?> e && actual instanceof List<?> a) {
      if (e.size() != a.size()) {
        return false;
      }
      for (int i = 0; i < e.size(); i++) {
        if (!same(e.get(i), a.get(i))) {
          return false;
        }
      }
      return true;
    }
    if (expected instanceof Map<?, ?> e && actual instanceof Map<?, ?> a) {
      if (!e.keySet().equals(a.keySet())) {
        return false;
      }
      for (Object key : e.keySet()) {
        if (!same(e.get(key), a.get(key))) {
          return false;
        }
      }
      return true;
    }
    return expected == null ? actual == null : expected.equals(actual);
  }

  private static Map<String, Object> reasonJson(Reason reason) {
    Map<String, Object> out = new java.util.LinkedHashMap<>();
    out.put("kind", reason.kind().name());
    if (reason.kind() == ReasonKind.RULE_MATCH) {
      out.put("ruleIndex", reason.ruleIndex().doubleValue());
    }
    if (reason.kind() == ReasonKind.PREREQUISITE_FAILED) {
      out.put("flagKey", reason.flagKey());
    }
    if (reason.kind() == ReasonKind.ERROR) {
      out.put("code", reason.errorCode().name());
    }
    return out;
  }

  @Test
  void everyVectorAndPin() throws IOException {
    List<Path> files = files();
    assertTrue(!files.isEmpty(), "no fixture files found in " + FIXTURES.toAbsolutePath());
    List<String> failures = new ArrayList<>();
    for (Path file : files) {
      Map<String, Object> doc = object(read(file));
      if (doc.containsKey("buckets")) {
        for (Object raw : array(doc.get("buckets"))) {
          Map<String, Object> pin = object(raw);
          String key = (String) pin.get("flagKey");
          String salt = (String) pin.get("salt");
          String contextKey = (String) pin.get("contextKey");
          long uint32 = Bucket.uint32(key, salt, contextKey);
          int bucket = Bucket.of(key, salt, contextKey);
          pins++;
          if (uint32 != ((Double) pin.get("uint32")).longValue()
              || bucket != ((Double) pin.get("bucket")).intValue()) {
            failures.add(file.getFileName() + " pin " + key + "/" + salt + "/" + contextKey);
          }
        }
        continue;
      }
      for (Object raw : array(doc.get("vectors"))) {
        Map<String, Object> vector = object(raw);
        vectors++;
        Map<String, Object> input = new java.util.LinkedHashMap<>();
        input.put("flags", vector.get("flags"));
        input.put("segments", vector.get("segments"));
        Ruleset ruleset = Ruleset.create(input);
        Result result =
            Evaluator.evaluate(ruleset, (String) vector.get("flagKey"), vector.get("context"));
        Map<String, Object> expected = object(vector.get("expected"));
        Object expectedIndex = expected.get("variationIndex");
        Object actualIndex =
            result.variationIndex() == null ? null : (Object) result.variationIndex().doubleValue();
        boolean ok =
            same(expected.get("value"), result.value())
                && same(expectedIndex, actualIndex)
                && same(expected.get("reason"), reasonJson(result.reason()));
        if (!ok) {
          failures.add(
              file.getFileName()
                  + ": "
                  + vector.get("name")
                  + " expected "
                  + expected
                  + " got value="
                  + result.value()
                  + " index="
                  + result.variationIndex()
                  + " reason="
                  + reasonJson(result.reason()));
        }
      }
    }
    assertTrue(vectors > 0, "read no vectors");
    assertTrue(pins > 0, "read no bucket pins");
    System.out.println("vectors: " + vectors + " pins: " + pins);
    if (!failures.isEmpty()) {
      fail(failures.size() + " failed:\n" + String.join("\n", failures));
    }
    assertEquals(0, failures.size());
  }
}
