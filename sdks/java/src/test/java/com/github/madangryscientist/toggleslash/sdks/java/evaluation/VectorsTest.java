package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
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

/** Runs every shared evaluation vector and every bucket pin. */
class VectorsTest {
  private static final Path DIR = Path.of("../../fixtures/evaluation");

  private static Object read(Path file) throws IOException {
    return Json.parse(Files.readAllBytes(file), 0, (int) Files.size(file));
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> object(Object value) {
    return (Map<String, Object>) value;
  }

  @SuppressWarnings("unchecked")
  private static List<Object> array(Object value) {
    return (List<Object>) value;
  }

  private static List<Path> files() throws IOException {
    try (Stream<Path> stream = Files.list(DIR)) {
      List<Path> out = new ArrayList<>(stream.filter(p -> p.toString().endsWith(".json")).toList());
      out.sort(Path::compareTo);
      return out;
    }
  }

  /** IEEE equality on numbers, deep equality elsewhere. */
  static boolean same(Object a, Object b) {
    if (a == null || b == null) {
      return a == b;
    }
    if (a instanceof Number x && b instanceof Number y) {
      return x.doubleValue() == y.doubleValue();
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
    if (a instanceof Map<?, ?> x && b instanceof Map<?, ?> y) {
      if (x.size() != y.size()) {
        return false;
      }
      for (Map.Entry<?, ?> e : x.entrySet()) {
        if (!y.containsKey(e.getKey()) || !same(e.getValue(), y.get(e.getKey()))) {
          return false;
        }
      }
      return true;
    }
    return a.equals(b);
  }

  @Test
  void everyVectorAndPin() throws IOException {
    int vectors = 0;
    int pins = 0;
    List<Path> files = files();
    assertFalse(files.isEmpty(), "no vector files under " + DIR);
    for (Path file : files) {
      Map<String, Object> doc = object(read(file));
      String name = file.getFileName().toString();
      if (name.equals("buckets.json")) {
        for (Object item : array(doc.get("buckets"))) {
          Map<String, Object> pin = object(item);
          String key = (String) pin.get("flagKey");
          String salt = (String) pin.get("salt");
          String contextKey = (String) pin.get("contextKey");
          long uint32 = Bucket.uint32(key, salt, contextKey);
          assertEquals(((Number) pin.get("uint32")).longValue(), uint32, name + " uint32 " + pin);
          assertEquals(
              ((Number) pin.get("bucket")).intValue(),
              Bucket.of(key, salt, contextKey),
              name + " bucket " + pin);
          pins++;
        }
        continue;
      }
      for (Object item : array(doc.get("vectors"))) {
        Map<String, Object> vector = object(item);
        String label = name + " / " + vector.get("name");
        Map<String, Object> source = new LinkedHashMap<>();
        source.put("flags", vector.get("flags"));
        source.put("segments", vector.get("segments"));
        Result result =
            Evaluator.evaluate(
                Ruleset.create(source), (String) vector.get("flagKey"), vector.get("context"));
        Map<String, Object> expected = object(vector.get("expected"));
        assertTrue(same(expected.get("value"), result.value()), label + " value " + result.value());
        Object index = expected.get("variationIndex");
        assertEquals(
            index == null ? null : ((Number) index).intValue(),
            result.variationIndex(),
            label + " variationIndex");
        assertTrue(
            same(expected.get("reason"), result.reason().toJson()),
            label + " reason " + result.reason().toJson());
        vectors++;
      }
    }
    assertTrue(vectors > 0, "no vectors ran");
    assertTrue(pins > 0, "no pins ran");
    String line = "vectors: " + vectors + " pins: " + pins;
    System.out.println(line);
    String counts = System.getenv("TOGGLESLASH_COUNTS_FILE");
    if (counts != null) {
      Files.writeString(Path.of(counts), line + "\n", StandardCharsets.UTF_8);
    }
  }
}
